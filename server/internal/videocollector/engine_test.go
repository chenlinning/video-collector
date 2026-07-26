package videocollector

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFindOutputFileUsesActualDirectoryEntry(t *testing.T) {
	directory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(directory, "output.mp4.part"), []byte("partial"), 0o600))
	expected := filepath.Join(directory, "output.mp4")
	require.NoError(t, os.WriteFile(expected, []byte("complete"), 0o600))

	require.Equal(t, expected, findOutputFile(directory))
	require.False(t, isRegularOutputFile(directory, filepath.Join(directory, "..", "outside.mp4")))
}

func TestRetryExtractorRetriesTransientFailures(t *testing.T) {
	attempts := 0
	value, _, err := retryExtractor(context.Background(), 3, 0, func() (string, string, error) {
		attempts++
		if attempts < 3 {
			return "", "ERROR: Unexpected response from webpage request", errors.New("exit status 1")
		}
		return "media", "", nil
	})

	require.NoError(t, err)
	require.Equal(t, "media", value)
	require.Equal(t, 3, attempts)
}

func TestRetryExtractorDoesNotRetryPermanentFailures(t *testing.T) {
	attempts := 0
	_, _, err := retryExtractor(context.Background(), 3, 0, func() (string, string, error) {
		attempts++
		return "", "ERROR: Unsupported URL", errors.New("exit status 1")
	})

	require.Error(t, err)
	require.Equal(t, 1, attempts)
}

func TestExtractorErrorClassifiesActionablePlatformFailures(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		kind   ExtractorFailureKind
	}{
		{name: "platform restriction", stderr: "ERROR: Unable to download webpage: HTTP Error 412: Precondition Failed", kind: ExtractorFailurePlatformRestricted},
		{name: "authentication", stderr: "ERROR: Cookies (not necessarily logged in) are needed", kind: ExtractorFailureAuthenticationRequired},
		{name: "drm", stderr: "ERROR: This video is DRM protected", kind: ExtractorFailureDRMProtected},
		{name: "unsupported", stderr: "ERROR: Unsupported URL: https://example.com/watch", kind: ExtractorFailureUnsupportedURL},
		{name: "unavailable", stderr: "ERROR: This video has been removed", kind: ExtractorFailureMediaUnavailable},
		{name: "temporary", stderr: "ERROR: HTTP Error 503: Service Unavailable", kind: ExtractorFailureTemporary},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := extractorError(tt.stderr, errors.New("exit status 1"))
			var failure *ExtractorFailure
			require.ErrorAs(t, err, &failure)
			require.Equal(t, tt.kind, failure.Kind)
			require.NotEmpty(t, failure.Error())
			require.NotContains(t, failure.Error(), "https://example.com/watch")
			require.NotContains(t, failure.Detail, "https://example.com/watch")
		})
	}
}

func TestRetryExtractorHonorsCancellationDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	_, _, err := retryExtractor(ctx, 3, time.Hour, func() (string, string, error) {
		attempts++
		cancel()
		return "", "ERROR: HTTP Error 429: Too Many Requests", errors.New("exit status 1")
	})

	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, attempts)
}

func TestBuildDownloadArgsSupportsMediaMP3AndSubtitles(t *testing.T) {
	direct := EgressDecision{Route: EgressDirect}
	media, err := buildDownloadArgs(DownloadRequest{Kind: TaskKindMedia, FormatID: "137", HasAudio: false}, "/tmp/task", "/usr/bin/ffmpeg", direct)
	require.NoError(t, err)
	require.Contains(t, media, "137+bestaudio/best")
	require.NotContains(t, media, "--extract-audio")
	require.NotContains(t, media, "--proxy")

	proxy := EgressDecision{Route: EgressCNProxy, proxyURL: "http://10.77.0.2:3128", connectTimeout: 5 * time.Second}
	audio, err := buildDownloadArgs(DownloadRequest{Kind: TaskKindAudio}, "/tmp/task", "/usr/bin/ffmpeg", proxy)
	require.NoError(t, err)
	require.Contains(t, audio, "--extract-audio")
	require.Contains(t, audio, "mp3")
	require.Equal(t, 1, countArgument(audio, "--proxy"))
	require.Contains(t, audio, "http://10.77.0.2:3128")

	subtitle, err := buildDownloadArgs(DownloadRequest{Kind: TaskKindSubtitle, ResourceID: "zh-Hans", Automatic: true}, "/tmp/task", "/usr/bin/ffmpeg", proxy)
	require.NoError(t, err)
	require.Contains(t, subtitle, "--write-auto-subs")
	require.Contains(t, subtitle, "zh-Hans")
	require.Contains(t, subtitle, "srt")
	require.Equal(t, 1, countArgument(subtitle, "--proxy"))

	_, err = buildDownloadArgs(DownloadRequest{Kind: TaskKindSubtitle, ResourceID: "../../etc/passwd"}, "/tmp/task", "/usr/bin/ffmpeg", direct)
	require.ErrorIs(t, err, ErrInvalidDownload)
}

func TestBuildParseAndTranscriptionArgsUseOnlyTrustedRouteProxy(t *testing.T) {
	direct := EgressDecision{Route: EgressDirect}
	proxy := EgressDecision{Route: EgressCNProxy, proxyURL: "http://10.77.0.2:3128", connectTimeout: 5 * time.Second}

	require.NotContains(t, buildParseArgs("https://example.com/video", "/usr/bin/ffmpeg", direct), "--proxy")
	require.Equal(t, 1, countArgument(buildParseArgs("https://example.com/video", "/usr/bin/ffmpeg", proxy), "--proxy"))
	require.NotContains(t, buildCollectionArgs("https://example.com/list", 10, direct), "--proxy")
	require.Equal(t, 1, countArgument(buildCollectionArgs("https://example.com/list", 10, proxy), "--proxy"))
	require.NotContains(t, buildTranscriptionDownloadArgs("https://example.com/video", "/tmp/task", "/usr/bin/ffmpeg", direct), "--proxy")
	require.Equal(t, 1, countArgument(buildTranscriptionDownloadArgs("https://example.com/video", "/tmp/task", "/usr/bin/ffmpeg", proxy), "--proxy"))
}

func countArgument(args []string, expected string) int {
	count := 0
	for _, arg := range args {
		if arg == expected {
			count++
		}
	}
	return count
}

func TestSelectMediaImageAndSafeExtension(t *testing.T) {
	media := &MediaInfo{Images: []MediaImage{
		{ID: "cover-small", URL: "https://cdn.example/small.jpg"},
		{ID: "cover-large", URL: "https://cdn.example/large.webp"},
	}}
	selected, err := selectMediaImage(media, "cover-large")
	require.NoError(t, err)
	require.Equal(t, "https://cdn.example/large.webp", selected.URL)

	_, err = selectMediaImage(media, "missing")
	require.ErrorIs(t, err, ErrInvalidDownload)
	require.Equal(t, "jpg", safeImageExtension("image/jpeg", "https://cdn.example/file"))
	require.Equal(t, "webp", safeImageExtension("application/octet-stream", "https://cdn.example/file.webp"))
	require.Empty(t, safeImageExtension("text/html", "https://cdn.example/file"))
}

func TestBuildTranscriptionCommandsUsesLocalInputAndModel(t *testing.T) {
	ffmpegArgs, whisperArgs, err := buildTranscriptionCommands(
		"/app/cache/tasks/task/input.mp4",
		"/app/cache/tasks/task",
		"/usr/bin/ffmpeg",
		"/usr/bin/whisper-cli",
		"/app/models/ggml-base.bin",
	)
	require.NoError(t, err)
	require.Contains(t, ffmpegArgs, "/app/cache/tasks/task/input.mp4")
	require.Contains(t, ffmpegArgs, "3600")
	require.Contains(t, whisperArgs, "/app/models/ggml-base.bin")
	require.Contains(t, whisperArgs, filepath.Join("/app/cache/tasks/task", "output"))

	_, _, err = buildTranscriptionCommands("", "/tmp/task", "ffmpeg", "whisper-cli", "model.bin")
	require.ErrorIs(t, err, ErrInvalidDownload)
}

func TestParseProbeDurationRejectsMediaLongerThanOneHour(t *testing.T) {
	duration, err := parseProbeDuration("3599.75\n")
	require.NoError(t, err)
	require.Equal(t, 3599.75, duration)

	_, err = parseProbeDuration("3600.01\n")
	require.ErrorIs(t, err, ErrMediaTooLong)

	_, err = parseProbeDuration("not-a-duration")
	require.Error(t, err)
}

func TestSafeHTTPClientUsesTrustedProxyAndRejectsPrivateRedirect(t *testing.T) {
	var requests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.URL.Path == "/redirect" {
			http.Redirect(response, request, "http://private.example/final", http.StatusFound)
			return
		}
		response.Header().Set("Content-Type", "image/png")
		_, _ = response.Write([]byte("image"))
	}))
	t.Cleanup(proxy.Close)

	resolver := hostResolverStub{addresses: map[string][]net.IPAddr{
		"media.example":   {{IP: net.ParseIP("203.0.113.10")}},
		"private.example": {{IP: net.ParseIP("10.0.0.8")}},
	}}
	decision := EgressDecision{Route: EgressCNProxy, proxyURL: proxy.URL, connectTimeout: time.Second}
	client, err := newSafeHTTPClient(resolver, decision)
	require.NoError(t, err)

	response, err := client.Get("http://media.example/image.png")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.NoError(t, response.Body.Close())
	require.Equal(t, int32(1), requests.Load())

	_, err = client.Get("http://media.example/redirect")
	require.ErrorIs(t, err, ErrUnsafeMediaURL)
	require.Equal(t, int32(2), requests.Load())
}

func TestSafeHTTPClientUsesHTTPConnectForHTTPS(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "image/png")
		_, _ = response.Write([]byte("secure-image"))
	}))
	t.Cleanup(target.Close)

	var connectRequests atomic.Int32
	proxy := newConnectProxy(t, target.Listener.Addr().String(), &connectRequests)
	t.Cleanup(proxy.Close)
	client, err := newSafeHTTPClient(
		hostResolverStub{addresses: map[string][]net.IPAddr{"example.com": {{IP: net.ParseIP("93.184.216.34")}}}},
		EgressDecision{Route: EgressCNProxy, proxyURL: proxy.URL, connectTimeout: time.Second},
	)
	require.NoError(t, err)
	rootCAs := x509.NewCertPool()
	rootCAs.AddCert(target.Certificate())
	client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: rootCAs, MinVersion: tls.VersionTLS12}

	response, err := client.Get("https://example.com/image.png")
	require.NoError(t, err)
	defer response.Body.Close()
	content, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, "secure-image", string(content))
	require.Equal(t, int32(1), connectRequests.Load())
}

func TestSafeHTTPClientDirectRouteRejectsPrivateDNS(t *testing.T) {
	resolver := hostResolverStub{addresses: map[string][]net.IPAddr{
		"private.example": {{IP: net.ParseIP("10.0.0.8")}},
	}}
	client, err := newSafeHTTPClient(resolver, EgressDecision{Route: EgressDirect})
	require.NoError(t, err)
	_, err = client.Get("http://private.example/image.png")
	require.ErrorIs(t, err, ErrUnsafeMediaURL)
}

func TestCleanupAttemptFilesRemovesOnlyTaskContents(t *testing.T) {
	parent := t.TempDir()
	taskDirectory := filepath.Join(parent, "task")
	require.NoError(t, os.Mkdir(taskDirectory, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(taskDirectory, "output.mp4.part"), []byte("partial"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(taskDirectory, "fragments"), 0o700))
	outside := filepath.Join(parent, "outside.txt")
	require.NoError(t, os.WriteFile(outside, []byte("keep"), 0o600))

	require.NoError(t, cleanupAttemptFiles(taskDirectory))
	entries, err := os.ReadDir(taskDirectory)
	require.NoError(t, err)
	require.Empty(t, entries)
	content, err := os.ReadFile(outside)
	require.NoError(t, err)
	require.Equal(t, "keep", strings.TrimSpace(string(content)))
}

type hostResolverStub struct {
	addresses map[string][]net.IPAddr
}

func newConnectProxy(t *testing.T, targetAddress string, requests *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodConnect {
			http.Error(response, "CONNECT required", http.StatusMethodNotAllowed)
			return
		}
		requests.Add(1)
		target, err := net.DialTimeout("tcp", targetAddress, time.Second)
		if err != nil {
			http.Error(response, "target unavailable", http.StatusBadGateway)
			return
		}
		client, _, err := response.(http.Hijacker).Hijack()
		if err != nil {
			_ = target.Close()
			return
		}
		_, _ = fmt.Fprint(client, "HTTP/1.1 200 Connection Established\r\n\r\n")
		go func() {
			defer target.Close()
			_, _ = io.Copy(target, client)
		}()
		_, _ = io.Copy(client, target)
		_ = client.Close()
	}))
}

func (r hostResolverStub) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	addresses := r.addresses[strings.ToLower(strings.TrimSuffix(host, "."))]
	if len(addresses) == 0 {
		return nil, errors.New("host not found")
	}
	return addresses, nil
}
