package webapp

import (
	"context"
	"net/http"
	"net/http/httptest"
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
	require.NotContains(t, start.Header().Get("Set-Cookie"), "session")
	require.Equal(t, http.StatusNotFound, requestStatus(handler, "/sso/entry?ticket=unused"))
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

func requestStatus(handler http.Handler, target string) int {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder.Code
}
