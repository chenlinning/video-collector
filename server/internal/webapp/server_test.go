package webapp

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chenlinning/video-collector/server/internal/videocollector"
	"github.com/stretchr/testify/require"
)

type serverEngineStub struct{}

func (serverEngineStub) Parse(context.Context, string) (*videocollector.MediaInfo, error) {
	return &videocollector.MediaInfo{
		ID: "media-1", SourceURL: "https://example.com/video", Title: "Video", Uploader: "Creator",
		Formats: []videocollector.MediaFormat{{ID: "best", Extension: "mp4", HasVideo: true, HasAudio: true}},
	}, nil
}

func (serverEngineStub) ParseCollection(_ context.Context, sourceURL string, limit int) (*videocollector.CollectionInfo, error) {
	return &videocollector.CollectionInfo{Title: "Collection", SourceURL: sourceURL, Items: []videocollector.CollectionItem{{ID: "item-1", SourceURL: "https://example.com/video/1", Title: "One"}}}, nil
}

func (serverEngineStub) Download(_ context.Context, _ videocollector.DownloadRequest, outputDir string, progress func(videocollector.ProgressUpdate)) (*videocollector.DownloadResult, error) {
	progress(videocollector.ProgressUpdate{State: videocollector.TaskStateDownloading, Percent: 50})
	output := filepath.Join(outputDir, "output.mp4")
	if err := os.WriteFile(output, []byte("video"), 0o600); err != nil {
		return nil, err
	}
	return &videocollector.DownloadResult{Path: output, Extension: "mp4"}, nil
}

func TestServerAllowsAnonymousAPIWithoutLogin(t *testing.T) {
	manager, err := videocollector.NewManager(videocollector.ManagerConfig{
		Root: t.TempDir(), MaxConcurrent: 1,
	}, serverEngineStub{})
	require.NoError(t, err)

	webRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("<html>collector</html>"), 0o600))
	handler, err := NewServer(ServerConfig{
		Manager: manager, WebRoot: webRoot,
		Runtime: RuntimeStatus{YTDLPVersion: "test", FFmpegVersion: "test"},
	})
	require.NoError(t, err)

	parseRequest := httptest.NewRequest(http.MethodPost, "/api/v1/media/parse", strings.NewReader(`{"url":"https://example.com/video"}`))
	parseRequest.Header.Set("Content-Type", "application/json")
	parsed := httptest.NewRecorder()
	handler.ServeHTTP(parsed, parseRequest)
	require.Equal(t, http.StatusOK, parsed.Code)
	require.Contains(t, parsed.Body.String(), "media-1")

	start := httptest.NewRecorder()
	startRequest := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(
		`{"sourceUrl":"https://example.com/video","mediaId":"media-1","title":"Video","formatId":"best","hasAudio":true}`,
	))
	startRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(start, startRequest)
	require.Equal(t, http.StatusAccepted, start.Code)
	requireTaskCompleted(t, manager, start.Body.Bytes())
	require.NotContains(t, start.Body.String(), `"deleteAt"`)
	require.NotContains(t, start.Body.String(), `"completedAt"`)
	require.NotContains(t, start.Header().Get("Set-Cookie"), "session")
	require.Equal(t, http.StatusNotFound, requestStatus(handler, "/sso/entry?ticket=unused"))
}

func TestHealthIncludesCompleteMediaRuntime(t *testing.T) {
	manager, err := videocollector.NewManager(videocollector.ManagerConfig{Root: t.TempDir(), MaxConcurrent: 1}, serverEngineStub{})
	require.NoError(t, err)
	webRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("<html>collector</html>"), 0o600))
	handler, err := NewServer(ServerConfig{
		Manager:      manager,
		WebRoot:      webRoot,
		Runtime:      RuntimeStatus{YTDLPVersion: "yt", FFmpegVersion: "ffmpeg", WhisperVersion: "whisper.cpp 1.8.6", WhisperModel: "ggml-base.bin"},
		EgressStatus: func() string { return "degraded" },
	})
	require.NoError(t, err)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "whisper.cpp 1.8.6")
	require.Contains(t, response.Body.String(), "ggml-base.bin")
	require.Contains(t, response.Body.String(), `"egressStatus":"degraded"`)
	require.NotContains(t, response.Body.String(), "10.77.0.2")
}

func TestServerAllowsEmbeddingFromOtherSites(t *testing.T) {
	manager, err := videocollector.NewManager(videocollector.ManagerConfig{
		Root: t.TempDir(), MaxConcurrent: 1,
	}, serverEngineStub{})
	require.NoError(t, err)

	webRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("<html>collector</html>"), 0o600))
	handler, err := NewServer(ServerConfig{Manager: manager, WebRoot: webRoot})
	require.NoError(t, err)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Header().Get("Content-Security-Policy"), "frame-ancestors *")
	require.Empty(t, response.Header().Get("X-Frame-Options"))
}

func TestServerSoftEmbedGateRequiresIframeBootstrapAndSession(t *testing.T) {
	manager, err := videocollector.NewManager(videocollector.ManagerConfig{
		Root: t.TempDir(), MaxConcurrent: 1,
	}, serverEngineStub{})
	require.NoError(t, err)

	webRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("<html>collector</html>"), 0o600))
	now := time.Date(2026, time.July, 26, 12, 0, 0, 0, time.UTC)
	handler, err := NewServer(ServerConfig{
		Manager:             manager,
		WebRoot:             webRoot,
		EmbedMode:           EmbedModeSoft,
		EmbedAllowedOrigins: []string{"https://ximoai.cn", "https://www.ximoai.cn"},
		EmbedSessionTTL:     time.Hour,
		Now:                 func() time.Time { return now },
	})
	require.NoError(t, err)

	direct := httptest.NewRecorder()
	handler.ServeHTTP(direct, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusForbidden, direct.Code)

	directAPI := httptest.NewRecorder()
	apiRequest := httptest.NewRequest(http.MethodPost, "/api/v1/media/parse", strings.NewReader(`{"url":"https://example.com/video"}`))
	apiRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(directAPI, apiRequest)
	require.Equal(t, http.StatusForbidden, directAPI.Code)

	bootstrapRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	bootstrapRequest.Header.Set("Sec-Fetch-Dest", "iframe")
	bootstrapRequest.Header.Set("Sec-Fetch-Site", "same-site")
	bootstrapRequest.Header.Set("Referer", "https://ximoai.cn/tools/video")
	bootstrap := httptest.NewRecorder()
	handler.ServeHTTP(bootstrap, bootstrapRequest)
	require.Equal(t, http.StatusOK, bootstrap.Code)
	require.Contains(t, bootstrap.Header().Get("Set-Cookie"), "vc_embed_session=")
	require.Contains(t, bootstrap.Header().Get("Content-Security-Policy"), "frame-ancestors https://www.ximoai.cn https://ximoai.cn")
	require.NotContains(t, bootstrap.Header().Get("Content-Security-Policy"), "frame-ancestors *")
	require.Empty(t, bootstrap.Header().Get("X-Frame-Options"))

	cookies := bootstrap.Result().Cookies()
	require.Len(t, cookies, 1)

	withSession := httptest.NewRecorder()
	withSessionRequest := httptest.NewRequest(http.MethodPost, "/api/v1/media/parse", strings.NewReader(`{"url":"https://example.com/video"}`))
	withSessionRequest.Header.Set("Content-Type", "application/json")
	withSessionRequest.AddCookie(cookies[0])
	handler.ServeHTTP(withSession, withSessionRequest)
	require.Equal(t, http.StatusOK, withSession.Code)

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health", nil))
	require.Equal(t, http.StatusOK, health.Code)
}

func TestServerSoftEmbedGateRejectsWrongParentAndDirectAssets(t *testing.T) {
	manager, err := videocollector.NewManager(videocollector.ManagerConfig{Root: t.TempDir(), MaxConcurrent: 1}, serverEngineStub{})
	require.NoError(t, err)
	webRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("<html>collector</html>"), 0o600))
	handler, err := NewServer(ServerConfig{
		Manager:             manager,
		WebRoot:             webRoot,
		EmbedMode:           EmbedModeSoft,
		EmbedAllowedOrigins: []string{"https://ximoai.cn"},
	})
	require.NoError(t, err)

	wrongParent := httptest.NewRequest(http.MethodGet, "/", nil)
	wrongParent.Header.Set("Sec-Fetch-Dest", "iframe")
	wrongParent.Header.Set("Sec-Fetch-Site", "cross-site")
	wrongParent.Header.Set("Referer", "https://attacker.example/iframe")
	wrongParentResult := httptest.NewRecorder()
	handler.ServeHTTP(wrongParentResult, wrongParent)
	require.Equal(t, http.StatusForbidden, wrongParentResult.Code)

	directAsset := httptest.NewRecorder()
	handler.ServeHTTP(directAsset, httptest.NewRequest(http.MethodGet, "/index.html", nil))
	require.Equal(t, http.StatusForbidden, directAsset.Code)
}

func TestServerRateLimitsAnonymousParseRequestsByIP(t *testing.T) {
	manager, err := videocollector.NewManager(videocollector.ManagerConfig{
		Root: t.TempDir(), MaxConcurrent: 1,
	}, serverEngineStub{})
	require.NoError(t, err)
	webRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("<html>collector</html>"), 0o600))
	handler, err := NewServer(ServerConfig{
		Manager: manager, WebRoot: webRoot,
		Runtime:        RuntimeStatus{YTDLPVersion: "test", FFmpegVersion: "test"},
		ParseRateLimit: 1,
	})
	require.NoError(t, err)

	first := httptest.NewRequest(http.MethodPost, "/api/v1/media/parse", strings.NewReader(`{"url":"https://example.com/video"}`))
	first.RemoteAddr = "203.0.113.10:1234"
	first.Header.Set("Content-Type", "application/json")
	firstResult := httptest.NewRecorder()
	handler.ServeHTTP(firstResult, first)
	require.Equal(t, http.StatusOK, firstResult.Code)

	second := httptest.NewRequest(http.MethodPost, "/api/v1/media/parse", strings.NewReader(`{"url":"https://example.com/video"}`))
	second.RemoteAddr = "203.0.113.10:5678"
	second.Header.Set("Content-Type", "application/json")
	secondResult := httptest.NewRecorder()
	handler.ServeHTTP(secondResult, second)
	require.Equal(t, http.StatusTooManyRequests, secondResult.Code)
	require.NotEmpty(t, secondResult.Header().Get("Retry-After"))
}

func TestServerRejectsNonJSONAndTrailingBodies(t *testing.T) {
	manager, err := videocollector.NewManager(videocollector.ManagerConfig{
		Root: t.TempDir(), MaxConcurrent: 1,
	}, serverEngineStub{})
	require.NoError(t, err)
	webRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("<html>collector</html>"), 0o600))
	handler, err := NewServer(ServerConfig{Manager: manager, WebRoot: webRoot})
	require.NoError(t, err)

	form := httptest.NewRequest(http.MethodPost, "/api/v1/media/parse", strings.NewReader("url=https://example.com/video"))
	form.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	formResult := httptest.NewRecorder()
	handler.ServeHTTP(formResult, form)
	require.Equal(t, http.StatusUnsupportedMediaType, formResult.Code)

	trailing := httptest.NewRequest(http.MethodPost, "/api/v1/media/parse", strings.NewReader(
		`{"url":"https://example.com/video"}{"extra":true}`,
	))
	trailing.Header.Set("Content-Type", "application/json")
	trailingResult := httptest.NewRecorder()
	handler.ServeHTTP(trailingResult, trailing)
	require.Equal(t, http.StatusBadRequest, trailingResult.Code)
}

func TestServerBatchParseAcceptsTenAndRejectsElevenURLs(t *testing.T) {
	manager, err := videocollector.NewManager(videocollector.ManagerConfig{
		Root: t.TempDir(), MaxConcurrent: 1,
	}, serverEngineStub{})
	require.NoError(t, err)
	webRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("<html>collector</html>"), 0o600))
	handler, err := NewServer(ServerConfig{Manager: manager, WebRoot: webRoot})
	require.NoError(t, err)

	valid := httptest.NewRequest(http.MethodPost, "/api/v1/media/batch", strings.NewReader(
		`{"urls":["https://example.com/one","https://example.com/two"]}`,
	))
	valid.Header.Set("Content-Type", "application/json")
	validResult := httptest.NewRecorder()
	handler.ServeHTTP(validResult, valid)
	require.Equal(t, http.StatusOK, validResult.Code)
	require.Contains(t, validResult.Body.String(), "https://example.com/one")

	overLimit := httptest.NewRequest(http.MethodPost, "/api/v1/media/batch", strings.NewReader(
		`{"urls":["1","2","3","4","5","6","7","8","9","10","11"]}`,
	))
	overLimit.Header.Set("Content-Type", "application/json")
	overLimitResult := httptest.NewRecorder()
	handler.ServeHTTP(overLimitResult, overLimit)
	require.Equal(t, http.StatusBadRequest, overLimitResult.Code)
}

func TestServerParsesPublicCollectionWithFixedTenItemLimit(t *testing.T) {
	manager, err := videocollector.NewManager(videocollector.ManagerConfig{Root: t.TempDir(), MaxConcurrent: 1}, serverEngineStub{})
	require.NoError(t, err)
	webRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("<html>collector</html>"), 0o600))
	handler, err := NewServer(ServerConfig{Manager: manager, WebRoot: webRoot})
	require.NoError(t, err)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/collections/parse", strings.NewReader(`{"url":"https://example.com/creator"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "item-1")
}

func TestServerAcceptsAnonymousTranscriptionUpload(t *testing.T) {
	manager, err := videocollector.NewManager(videocollector.ManagerConfig{Root: t.TempDir(), MaxConcurrent: 1}, serverEngineStub{})
	require.NoError(t, err)
	webRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("<html>collector</html>"), 0o600))
	handler, err := NewServer(ServerConfig{Manager: manager, WebRoot: webRoot})
	require.NoError(t, err)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "sample.mp3")
	require.NoError(t, err)
	_, err = part.Write([]byte("audio-data"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	request := httptest.NewRequest(http.MethodPost, "/api/v1/transcriptions/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusAccepted, response.Code)
	requireTaskCompleted(t, manager, response.Body.Bytes())
	require.NotContains(t, response.Header().Get("Set-Cookie"), "session")
}

func TestServerExtractsTextDocumentInMemory(t *testing.T) {
	manager, err := videocollector.NewManager(videocollector.ManagerConfig{Root: t.TempDir(), MaxConcurrent: 1}, serverEngineStub{})
	require.NoError(t, err)
	webRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("<html>collector</html>"), 0o600))
	handler, err := NewServer(ServerConfig{Manager: manager, WebRoot: webRoot})
	require.NoError(t, err)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "article.txt")
	require.NoError(t, err)
	_, err = part.Write([]byte("第一行，第二行！！！"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	request := httptest.NewRequest(http.MethodPost, "/api/v1/text/extract", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.JSONEq(t, `{"fileName":"article.txt","format":"txt","text":"第一行，第二行！！！","byteSize":30,"characterCount":10}`, response.Body.String())
}

func TestServerRejectsOversizedTextDocumentWithPayloadTooLarge(t *testing.T) {
	manager, err := videocollector.NewManager(videocollector.ManagerConfig{Root: t.TempDir(), MaxConcurrent: 1}, serverEngineStub{})
	require.NoError(t, err)
	webRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("<html>collector</html>"), 0o600))
	handler, err := NewServer(ServerConfig{Manager: manager, WebRoot: webRoot})
	require.NoError(t, err)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "oversized.txt")
	require.NoError(t, err)
	_, err = part.Write(bytes.Repeat([]byte{'a'}, int(MaxTextDocumentBytes+2<<20)))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	request := httptest.NewRequest(http.MethodPost, "/api/v1/text/extract", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
}

func TestServerRejectsTranscriptionUploadWithNonMediaMIME(t *testing.T) {
	manager, err := videocollector.NewManager(videocollector.ManagerConfig{Root: t.TempDir(), MaxConcurrent: 1}, serverEngineStub{})
	require.NoError(t, err)
	webRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("<html>collector</html>"), 0o600))
	handler, err := NewServer(ServerConfig{Manager: manager, WebRoot: webRoot})
	require.NoError(t, err)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="sample.mp3"`)
	header.Set("Content-Type", "text/html")
	part, err := writer.CreatePart(header)
	require.NoError(t, err)
	_, err = part.Write([]byte("<html>not audio</html>"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	request := httptest.NewRequest(http.MethodPost, "/api/v1/transcriptions/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusUnsupportedMediaType, response.Code)
}

func requestStatus(handler http.Handler, target string) int {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder.Code
}

func requireTaskCompleted(t *testing.T, manager *videocollector.Manager, responseBody []byte) {
	t.Helper()
	var started videocollector.TaskSnapshot
	require.NoError(t, json.Unmarshal(responseBody, &started))
	require.Eventually(t, func() bool {
		snapshot, err := manager.Get(started.ID)
		return err == nil && snapshot.State == videocollector.TaskStateCompleted
	}, time.Second, time.Millisecond)
}
