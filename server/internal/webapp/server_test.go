package webapp

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		Manager: manager,
		WebRoot: webRoot,
		Runtime: RuntimeStatus{YTDLPVersion: "yt", FFmpegVersion: "ffmpeg", WhisperVersion: "whisper.cpp 1.8.6", WhisperModel: "ggml-base.bin"},
	})
	require.NoError(t, err)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "whisper.cpp 1.8.6")
	require.Contains(t, response.Body.String(), "ggml-base.bin")
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
	require.NotContains(t, response.Header().Get("Set-Cookie"), "session")
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
