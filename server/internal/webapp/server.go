package webapp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/chenlinning/video-collector/server/internal/videocollector"
)

type RuntimeStatus struct {
	YTDLPVersion  string `json:"ytDlpVersion"`
	FFmpegVersion string `json:"ffmpegVersion"`
}

type ServerConfig struct {
	Manager         *videocollector.Manager
	WebRoot         string
	Runtime         RuntimeStatus
	ParseRateLimit  int
	TaskRateLimit   int
	ParseRateWindow time.Duration
	TaskRateWindow  time.Duration
	TrustProxy      bool
	Now             func() time.Time
}

type Server struct {
	manager      *videocollector.Manager
	webRoot      string
	runtime      RuntimeStatus
	trustProxy   bool
	parseLimiter *rateLimiter
	taskLimiter  *rateLimiter
	mux          *http.ServeMux
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
		mux:          http.NewServeMux(),
	}
	server.routes()
	return server, nil
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /api/v1/status", s.handleStatus)
	s.mux.HandleFunc("POST /api/v1/media/parse", s.withRateLimit(s.parseLimiter, s.handleParse))
	s.mux.HandleFunc("POST /api/v1/tasks", s.withRateLimit(s.taskLimiter, s.handleStart))
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
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; font-src 'self' data:; connect-src 'self'; frame-ancestors 'self' https://ximoai.cn; base-uri 'self'; form-action 'none'")
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Cache-Control", "no-store")
	}
	s.mux.ServeHTTP(w, r)
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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		_, _ = io.WriteString(w, fmt.Sprintf(`{"code":%s}`, strconv.Itoa(status)))
	}
}
