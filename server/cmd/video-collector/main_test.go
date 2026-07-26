package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoadConfigAllowsSixtyMinuteTranscriptionsToFinish(t *testing.T) {
	for _, name := range []string{
		"VIDEO_COLLECTOR_MAX_CONCURRENT", "VIDEO_COLLECTOR_MAX_QUEUED",
		"VIDEO_COLLECTOR_PARSE_RATE_LIMIT", "VIDEO_COLLECTOR_TASK_RATE_LIMIT",
		"VIDEO_COLLECTOR_TASK_TIMEOUT_SECONDS",
	} {
		t.Setenv(name, "")
	}

	config, err := loadConfig()
	require.NoError(t, err)
	require.Equal(t, 2*time.Hour, config.taskTimeout)
}
