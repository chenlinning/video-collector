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
