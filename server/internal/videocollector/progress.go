package videocollector

import (
	"strconv"
	"strings"
)

func ParseProgressLine(line string) (ProgressUpdate, bool) {
	text := strings.TrimSpace(line)
	if strings.HasPrefix(text, "__VC_PROGRESS__:") {
		parts := strings.Split(strings.TrimPrefix(text, "__VC_PROGRESS__:"), "|")
		if len(parts) < 5 {
			return ProgressUpdate{}, false
		}
		percent, _ := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(parts[0], "%")), 64)
		if percent < 0 {
			percent = 0
		}
		if percent > 100 {
			percent = 100
		}
		return ProgressUpdate{
			State:           TaskStateDownloading,
			Percent:         percent,
			Speed:           optionalProgressText(parts[1]),
			ETA:             optionalProgressText(parts[2]),
			DownloadedBytes: parseProgressNumber(parts[3]),
			TotalBytes:      parseProgressNumber(parts[4]),
		}, true
	}
	if strings.HasPrefix(text, "__VC_PROCESSING__:") {
		return ProgressUpdate{State: TaskStateProcessing, Percent: 99}, true
	}
	return ProgressUpdate{}, false
}

func optionalProgressText(value string) string {
	value = strings.TrimSpace(value)
	if value == "NA" || value == "Unknown" {
		return ""
	}
	return value
}

func parseProgressNumber(value string) int64 {
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return parsed
}
