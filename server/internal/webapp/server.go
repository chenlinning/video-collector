package webapp

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chenlinning/video-collector/server/internal/videocollector"
)

type RuntimeStatus struct {
	YTDLPVersion   string `json:"ytDlpVersion"`
	FFmpegVersion  string `json:"ffmpegVersion"`
	WhisperVersion string `json:"whisperVersion"`
	WhisperModel   string `json:"whisperModel"`
}

const (
	EmbedModeOff  = "off"
	EmbedModeSoft = "soft"
)

const embedSessionCookieName = "vc_embed_session"

type ServerConfig struct {
	Manager             *videocollector.Manager
	WebRoot             string
	Runtime             RuntimeStatus
	ParseRateLimit      int
	TaskRateLimit       int
	ParseRateWindow     time.Duration
	TaskRateWindow      time.Duration
	TrustProxy          bool
	EgressStatus        func() string
	Now                 func() time.Time
	EmbedMode           string
	EmbedAllowedOrigins []string
	EmbedSessionTTL     time.Duration
}

type Server struct {
	manager       *videocollector.Manager
	webRoot       string
	runtime       RuntimeStatus
	trustProxy    bool
	parseLimiter  *rateLimiter
	taskLimiter   *rateLimiter
	egressStatus  func() string
	mux           *http.ServeMux
	embedMode     string
	embedOrigins  map[string]struct{}
	embedTTL      time.Duration
	now           func() time.Time
	embedMu       sync.Mutex
	embedSessions map[string]time.Time
}

func NewServer(config ServerConfig) (*Server, error) {
	if config.Manager == nil {
		return nil, errors.New("video collector server dependencies are required")
	}
	if config.ParseRateLimit <= 0 {
		config.ParseRateLimit = 20
	}
	if config.TaskRateLimit <= 0 {
		config.TaskRateLimit = 5
	}
	if config.ParseRateWindow <= 0 {
		config.ParseRateWindow = 15 * time.Minute
	}
	if config.TaskRateWindow <= 0 {
		config.TaskRateWindow = time.Hour
	}
	if config.EgressStatus == nil {
		config.EgressStatus = func() string { return "off" }
	}
	if config.EmbedMode == "" {
		config.EmbedMode = EmbedModeOff
	}
	if config.EmbedMode != EmbedModeOff && config.EmbedMode != EmbedModeSoft {
		return nil, errors.New("embed mode must be off or soft")
	}
	if config.EmbedSessionTTL <= 0 {
		config.EmbedSessionTTL = time.Hour
	}
	embedOrigins, err := normalizeEmbedOrigins(config.EmbedAllowedOrigins)
	if err != nil {
		return nil, err
	}
	if config.EmbedMode == EmbedModeSoft && len(embedOrigins) == 0 {
		return nil, errors.New("soft embed mode requires allowed parent origins")
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	webRoot, err := filepath.Abs(strings.TrimSpace(config.WebRoot))
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(filepath.Join(webRoot, "index.html")); err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("video collector web root is invalid")
	}
	server := &Server{
		manager: config.Manager, webRoot: webRoot, runtime: config.Runtime,
		trustProxy:   config.TrustProxy,
		parseLimiter: newRateLimiter(config.ParseRateLimit, config.ParseRateWindow, config.Now),
		taskLimiter:  newRateLimiter(config.TaskRateLimit, config.TaskRateWindow, config.Now),
		egressStatus: config.EgressStatus,
		mux:          http.NewServeMux(), embedMode: config.EmbedMode,
		embedOrigins: embedOrigins, embedTTL: config.EmbedSessionTTL, now: now,
		embedSessions: make(map[string]time.Time),
	}
	server.routes()
	return server, nil
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /api/v1/status", s.handleStatus)
	s.mux.HandleFunc("POST /api/v1/media/parse", s.withRateLimit(s.parseLimiter, s.handleParse))
	s.mux.HandleFunc("POST /api/v1/media/batch", s.withRateLimit(s.parseLimiter, s.handleBatchParse))
	s.mux.HandleFunc("POST /api/v1/collections/parse", s.withRateLimit(s.parseLimiter, s.handleCollectionParse))
	s.mux.HandleFunc("POST /api/v1/tasks", s.withRateLimit(s.taskLimiter, s.handleStart))
	s.mux.HandleFunc("POST /api/v1/transcriptions/upload", s.withRateLimit(s.taskLimiter, s.handleTranscriptionUpload))
	s.mux.HandleFunc("GET /api/v1/tasks/{id}", s.handleGetTask)
	s.mux.HandleFunc("DELETE /api/v1/tasks/{id}", s.handleCancelTask)
	s.mux.HandleFunc("GET /api/v1/tasks/{id}/download", s.handleDownload)
	s.mux.HandleFunc("GET /api/", http.NotFound)
	s.mux.HandleFunc("POST /api/", http.NotFound)
	s.mux.HandleFunc("DELETE /api/", http.NotFound)
	s.mux.HandleFunc("GET /sso/", http.NotFound)
	s.mux.HandleFunc("GET /", s.handleStatic)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	frameAncestors := "*"
	if s.embedMode == EmbedModeSoft {
		origins := make([]string, 0, len(s.embedOrigins))
		for origin := range s.embedOrigins {
			origins = append(origins, origin)
		}
		// Sorting keeps the header deterministic for tests and cache keys.
		sort.Strings(origins)
		frameAncestors = strings.Join(origins, " ")
		w.Header().Set("Vary", "Sec-Fetch-Dest, Sec-Fetch-Site, Origin, Referer, Cookie")
	}
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; font-src 'self' data:; connect-src 'self'; frame-ancestors "+frameAncestors+"; base-uri 'self'; form-action 'none'")
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Cache-Control", "no-store")
	}
	if s.embedMode == EmbedModeSoft && !s.embedAccess(w, r) {
		return
	}
	s.mux.ServeHTTP(w, r)
}

func normalizeEmbedOrigins(values []string) (map[string]struct{}, error) {
	origins := make(map[string]struct{}, len(values))
	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return nil, errors.New("embed allowed origins contain an empty value")
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.User != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, fmt.Errorf("invalid embed allowed origin %q", raw)
		}
		origins[strings.ToLower(parsed.Scheme)+"://"+strings.ToLower(parsed.Host)] = struct{}{}
	}
	return origins, nil
}

func (s *Server) embedAccess(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path == "/health" {
		return true
	}
	if s.hasEmbedSession(r) {
		return true
	}
	if r.Method == http.MethodGet && (r.URL.Path == "/" || r.URL.Path == "/index.html") && s.isAllowedIframeRequest(r) {
		return s.issueEmbedSession(w)
	}
	writeJSONError(w, http.StatusForbidden, "仅允许通过主站嵌入页面访问")
	return false
}

func (s *Server) isAllowedIframeRequest(r *http.Request) bool {
	if strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Dest"))) != "iframe" {
		return false
	}
	fetchSite := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")))
	if fetchSite != "same-site" && fetchSite != "same-origin" {
		return false
	}
	seenParent := false
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		seenParent = true
		if !s.isAllowedOrigin(origin) {
			return false
		}
	}
	if referer := strings.TrimSpace(r.Header.Get("Referer")); referer != "" {
		seenParent = true
		parsed, err := url.Parse(referer)
		if err != nil || !s.isAllowedOrigin(parsed.Scheme+"://"+parsed.Host) {
			return false
		}
	}
	return seenParent
}

func (s *Server) isAllowedOrigin(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	origin := strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host)
	_, ok := s.embedOrigins[origin]
	return ok
}

func (s *Server) hasEmbedSession(r *http.Request) bool {
	cookie, err := r.Cookie(embedSessionCookieName)
	if err != nil || cookie.Value == "" {
		return false
	}
	now := s.now()
	s.embedMu.Lock()
	defer s.embedMu.Unlock()
	expires, ok := s.embedSessions[cookie.Value]
	if !ok || !now.Before(expires) {
		delete(s.embedSessions, cookie.Value)
		return false
	}
	return true
}

func (s *Server) issueEmbedSession(w http.ResponseWriter) bool {
	var tokenBytes [32]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "无法建立嵌入会话")
		return false
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes[:])
	expires := s.now().Add(s.embedTTL)
	s.embedMu.Lock()
	for value, expiry := range s.embedSessions {
		if !s.now().Before(expiry) {
			delete(s.embedSessions, value)
		}
	}
	s.embedSessions[token] = expires
	s.embedMu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name: embedSessionCookieName, Value: token, Path: "/", MaxAge: max(1, int(s.embedTTL.Seconds())),
		Expires: expires, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
	return true
}

func (s *Server) withRateLimit(limiter *rateLimiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		allowed, retryAfter := limiter.allow(clientIP(r, s.trustProxy))
		if !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(max(1, int(retryAfter.Seconds()))))
			writeJSONError(w, http.StatusTooManyRequests, "请求过于频繁，请稍后重试")
			return
		}
		next(w, r)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok", "ytDlpVersion": s.runtime.YTDLPVersion, "ffmpegVersion": s.runtime.FFmpegVersion,
		"whisperVersion": s.runtime.WhisperVersion, "whisperModel": s.runtime.WhisperModel,
		"egressStatus": s.egressStatus(),
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.runtime)
}

func (s *Server) handleParse(w http.ResponseWriter, r *http.Request) {
	var input struct {
		URL string `json:"url"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	info, err := s.manager.Parse(r.Context(), input.URL)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, safeError(err))
		return
	}
	writeJSON(w, http.StatusOK, info)
}

type batchParseItem struct {
	URL   string                    `json:"url"`
	Media *videocollector.MediaInfo `json:"media,omitempty"`
	Error string                    `json:"error,omitempty"`
}

func (s *Server) handleBatchParse(w http.ResponseWriter, r *http.Request) {
	var input struct {
		URLs []string `json:"urls"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	if len(input.URLs) == 0 || len(input.URLs) > 10 {
		writeJSONError(w, http.StatusBadRequest, "batch must contain between 1 and 10 URLs")
		return
	}
	results := make([]batchParseItem, 0, len(input.URLs))
	for _, rawURL := range input.URLs {
		item := batchParseItem{URL: strings.TrimSpace(rawURL)}
		if item.URL == "" {
			item.Error = "URL is required"
			results = append(results, item)
			continue
		}
		media, err := s.manager.Parse(r.Context(), item.URL)
		if err != nil {
			item.Error = safeError(err)
		} else {
			item.Media = media
		}
		results = append(results, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": results})
}

func (s *Server) handleCollectionParse(w http.ResponseWriter, r *http.Request) {
	var input struct {
		URL string `json:"url"`
	}
	if !decodeJSONBody(w, r, &input) {
		return
	}
	collection, err := s.manager.ParseCollection(r.Context(), input.URL, 10)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, safeError(err))
		return
	}
	writeJSON(w, http.StatusOK, collection)
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	var input videocollector.DownloadRequest
	if !decodeJSONBody(w, r, &input) {
		return
	}
	task, err := s.manager.Start(input)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, videocollector.ErrTaskQueueFull) {
			status = http.StatusTooManyRequests
		}
		writeJSONError(w, status, safeError(err))
		return
	}
	writeJSON(w, http.StatusAccepted, task)
}

func (s *Server) handleTranscriptionUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, videocollector.MaxUploadBytes+1024*1024)
	reader, err := r.MultipartReader()
	if err != nil {
		writeJSONError(w, http.StatusUnsupportedMediaType, "content type must be multipart/form-data")
		return
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "file" || strings.TrimSpace(part.FileName()) == "" {
		writeJSONError(w, http.StatusBadRequest, "a media file is required")
		return
	}
	defer part.Close()
	if !validUploadMediaType(part.Header.Get("Content-Type")) {
		writeJSONError(w, http.StatusUnsupportedMediaType, "file must be audio or video")
		return
	}
	task, err := s.manager.StartUpload(part.FileName(), part)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, videocollector.ErrTaskQueueFull) {
			status = http.StatusTooManyRequests
		}
		writeJSONError(w, status, safeError(err))
		return
	}
	writeJSON(w, http.StatusAccepted, task)
}

func validUploadMediaType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return false
	}
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	return strings.HasPrefix(mediaType, "audio/") || strings.HasPrefix(mediaType, "video/") || mediaType == "application/octet-stream"
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	task, err := s.manager.Get(r.PathValue("id"))
	if err != nil {
		writeManagerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	if err := s.manager.Cancel(r.PathValue("id")); err != nil {
		writeManagerError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	lease, err := s.manager.OpenDownload(r.PathValue("id"))
	if err != nil {
		writeManagerError(w, err)
		return
	}
	defer func() { _ = lease.Close() }()
	info, err := lease.Stat()
	if err != nil {
		writeJSONError(w, http.StatusGone, "download file is no longer available")
		return
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": lease.FileName}))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("X-Delete-At", lease.DeleteAt.UTC().Format("2006-01-02T15:04:05Z"))
	http.ServeContent(w, r, lease.FileName, info.ModTime(), lease.File)
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	requested := strings.TrimPrefix(filepath.Clean("/"+r.URL.Path), "/")
	if requested == "." || requested == "" {
		requested = "index.html"
	}
	candidate := filepath.Join(s.webRoot, filepath.FromSlash(requested))
	if relative, err := filepath.Rel(s.webRoot, candidate); err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		http.NotFound(w, r)
		return
	}
	if info, err := os.Stat(candidate); err != nil || !info.Mode().IsRegular() {
		candidate = filepath.Join(s.webRoot, "index.html")
	}
	if filepath.Base(candidate) == "index.html" {
		w.Header().Set("Cache-Control", "no-cache")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	if s.embedMode == EmbedModeSoft {
		w.Header().Set("Cache-Control", "no-store")
	}
	http.ServeFile(w, r, candidate)
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, target any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeJSONError(w, http.StatusUnsupportedMediaType, "content type must be application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func writeManagerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, videocollector.ErrTaskNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, videocollector.ErrTaskNotReady):
		writeJSONError(w, http.StatusGone, err.Error())
	default:
		writeJSONError(w, http.StatusBadRequest, safeError(err))
	}
}

func safeError(err error) string {
	if err == nil {
		return "operation failed"
	}
	message := strings.TrimSpace(err.Error())
	if len(message) > 500 {
		message = message[:500]
	}
	return message
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"code": status, "message": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		_, _ = io.WriteString(w, fmt.Sprintf(`{"code":%s}`, strconv.Itoa(status)))
	}
}
