package videocollector

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseProgressLine(t *testing.T) {
	update, ok := ParseProgressLine("__VC_PROGRESS__:42.5%|1.20MiB/s|00:10|6451200|15217445")

	require.True(t, ok)
	require.Equal(t, TaskStateDownloading, update.State)
	require.Equal(t, 42.5, update.Percent)
	require.Equal(t, "1.20MiB/s", update.Speed)
	require.Equal(t, "00:10", update.ETA)
	require.EqualValues(t, 6451200, update.DownloadedBytes)
	require.EqualValues(t, 15217445, update.TotalBytes)
}

func TestParseProgressLineHandlesProcessing(t *testing.T) {
	update, ok := ParseProgressLine("__VC_PROCESSING__:merging")
	require.True(t, ok)
	require.Equal(t, TaskStateProcessing, update.State)
}
