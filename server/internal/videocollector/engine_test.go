package videocollector

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
	media, err := buildDownloadArgs(DownloadRequest{Kind: TaskKindMedia, FormatID: "137", HasAudio: false}, "/tmp/task", "/usr/bin/ffmpeg")
	require.NoError(t, err)
	require.Contains(t, media, "137+bestaudio/best")
	require.NotContains(t, media, "--extract-audio")

	audio, err := buildDownloadArgs(DownloadRequest{Kind: TaskKindAudio}, "/tmp/task", "/usr/bin/ffmpeg")
	require.NoError(t, err)
	require.Contains(t, audio, "--extract-audio")
	require.Contains(t, audio, "mp3")

	subtitle, err := buildDownloadArgs(DownloadRequest{Kind: TaskKindSubtitle, ResourceID: "zh-Hans", Automatic: true}, "/tmp/task", "/usr/bin/ffmpeg")
	require.NoError(t, err)
	require.Contains(t, subtitle, "--write-auto-subs")
	require.Contains(t, subtitle, "zh-Hans")
	require.Contains(t, subtitle, "srt")

	_, err = buildDownloadArgs(DownloadRequest{Kind: TaskKindSubtitle, ResourceID: "../../etc/passwd"}, "/tmp/task", "/usr/bin/ffmpeg")
	require.ErrorIs(t, err, ErrInvalidDownload)
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
