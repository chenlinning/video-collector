package videocollector

import (
	"context"
	"time"
)

type MediaFormat struct {
	ID               string `json:"id"`
	Label            string `json:"label"`
	Extension        string `json:"extension"`
	Width            int    `json:"width,omitempty"`
	Height           int    `json:"height,omitempty"`
	VideoCodec       string `json:"videoCodec,omitempty"`
	AudioCodec       string `json:"audioCodec,omitempty"`
	ApproximateBytes int64  `json:"approximateBytes,omitempty"`
	BitrateKbps      int64  `json:"bitrateKbps,omitempty"`
	HasVideo         bool   `json:"hasVideo"`
	HasAudio         bool   `json:"hasAudio"`
}

type MediaInfo struct {
	ID        string        `json:"id"`
	SourceURL string        `json:"sourceUrl"`
	Title     string        `json:"title"`
	Uploader  string        `json:"uploader"`
	Thumbnail string        `json:"thumbnail,omitempty"`
	Duration  float64       `json:"duration,omitempty"`
	Extractor string        `json:"extractor"`
	Formats   []MediaFormat `json:"formats"`
}

type DownloadRequest struct {
	SourceURL string `json:"sourceUrl"`
	MediaID   string `json:"mediaId"`
	Title     string `json:"title"`
	FormatID  string `json:"formatId"`
	HasAudio  bool   `json:"hasAudio"`
}

type TaskState string

const (
	TaskStateQueued      TaskState = "queued"
	TaskStateDownloading TaskState = "downloading"
	TaskStateProcessing  TaskState = "processing"
	TaskStateCompleted   TaskState = "completed"
	TaskStateCancelled   TaskState = "cancelled"
	TaskStateFailed      TaskState = "failed"
	TaskStateExpired     TaskState = "expired"
)

type TaskSnapshot struct {
	ID              string    `json:"id"`
	State           TaskState `json:"state"`
	Percent         float64   `json:"percent"`
	Speed           string    `json:"speed,omitempty"`
	ETA             string    `json:"eta,omitempty"`
	DownloadedBytes int64     `json:"downloadedBytes,omitempty"`
	TotalBytes      int64     `json:"totalBytes,omitempty"`
	FileName        string    `json:"fileName,omitempty"`
	FileSize        int64     `json:"fileSize,omitempty"`
	Error           string    `json:"error,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	CompletedAt     time.Time `json:"completedAt,omitempty"`
	DeleteAt        time.Time `json:"deleteAt,omitempty"`
}

type ProgressUpdate struct {
	State           TaskState
	Percent         float64
	Speed           string
	ETA             string
	DownloadedBytes int64
	TotalBytes      int64
}

type DownloadResult struct {
	Path      string
	Extension string
}

type Engine interface {
	Parse(ctx context.Context, sourceURL string) (*MediaInfo, error)
	Download(ctx context.Context, request DownloadRequest, outputDir string, progress func(ProgressUpdate)) (*DownloadResult, error)
}
