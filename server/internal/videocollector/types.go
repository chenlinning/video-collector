package videocollector

import (
	"context"
	"encoding/json"
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

type MediaImage struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	Extension string `json:"extension,omitempty"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
}

type SubtitleTrack struct {
	Language  string `json:"language"`
	Name      string `json:"name,omitempty"`
	Extension string `json:"extension,omitempty"`
	Automatic bool   `json:"automatic"`
}

type MediaMetrics struct {
	Views    int64 `json:"views,omitempty"`
	Likes    int64 `json:"likes,omitempty"`
	Comments int64 `json:"comments,omitempty"`
	Reposts  int64 `json:"reposts,omitempty"`
}

type CollectionItem struct {
	ID        string       `json:"id"`
	SourceURL string       `json:"sourceUrl"`
	Title     string       `json:"title"`
	Thumbnail string       `json:"thumbnail,omitempty"`
	Duration  float64      `json:"duration,omitempty"`
	Metrics   MediaMetrics `json:"metrics"`
}

type CollectionInfo struct {
	ID        string           `json:"id"`
	SourceURL string           `json:"sourceUrl"`
	Title     string           `json:"title"`
	Uploader  string           `json:"uploader,omitempty"`
	Items     []CollectionItem `json:"items"`
}

type MediaInfo struct {
	ID        string          `json:"id"`
	SourceURL string          `json:"sourceUrl"`
	Title     string          `json:"title"`
	Uploader  string          `json:"uploader"`
	Thumbnail string          `json:"thumbnail,omitempty"`
	Duration  float64         `json:"duration,omitempty"`
	Extractor string          `json:"extractor"`
	Formats   []MediaFormat   `json:"formats"`
	Images    []MediaImage    `json:"images,omitempty"`
	Subtitles []SubtitleTrack `json:"subtitles,omitempty"`
	Metrics   MediaMetrics    `json:"metrics"`
}

type TaskKind string

const (
	TaskKindMedia      TaskKind = "media"
	TaskKindAudio      TaskKind = "audio"
	TaskKindImage      TaskKind = "image"
	TaskKindSubtitle   TaskKind = "subtitle"
	TaskKindTranscript TaskKind = "transcript"
)

type DownloadRequest struct {
	SourceURL  string   `json:"sourceUrl"`
	MediaID    string   `json:"mediaId"`
	Title      string   `json:"title"`
	FormatID   string   `json:"formatId"`
	HasAudio   bool     `json:"hasAudio"`
	Kind       TaskKind `json:"kind,omitempty"`
	ResourceID string   `json:"resourceId,omitempty"`
	Automatic  bool     `json:"automatic,omitempty"`
	InputPath  string   `json:"-"`
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
	Kind            TaskKind  `json:"kind"`
	State           TaskState `json:"state"`
	Percent         float64   `json:"percent"`
	Speed           string    `json:"speed,omitempty"`
	ETA             string    `json:"eta,omitempty"`
	DownloadedBytes int64     `json:"downloadedBytes,omitempty"`
	TotalBytes      int64     `json:"totalBytes,omitempty"`
	FileName        string    `json:"fileName,omitempty"`
	FileSize        int64     `json:"fileSize,omitempty"`
	TextPreview     string    `json:"textPreview,omitempty"`
	Error           string    `json:"error,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	CompletedAt     time.Time `json:"completedAt,omitempty"`
	DeleteAt        time.Time `json:"deleteAt,omitempty"`
}

func (snapshot TaskSnapshot) MarshalJSON() ([]byte, error) {
	type taskSnapshotJSON struct {
		ID              string     `json:"id"`
		Kind            TaskKind   `json:"kind"`
		State           TaskState  `json:"state"`
		Percent         float64    `json:"percent"`
		Speed           string     `json:"speed,omitempty"`
		ETA             string     `json:"eta,omitempty"`
		DownloadedBytes int64      `json:"downloadedBytes,omitempty"`
		TotalBytes      int64      `json:"totalBytes,omitempty"`
		FileName        string     `json:"fileName,omitempty"`
		FileSize        int64      `json:"fileSize,omitempty"`
		TextPreview     string     `json:"textPreview,omitempty"`
		Error           string     `json:"error,omitempty"`
		CreatedAt       time.Time  `json:"createdAt"`
		CompletedAt     *time.Time `json:"completedAt,omitempty"`
		DeleteAt        *time.Time `json:"deleteAt,omitempty"`
	}
	value := taskSnapshotJSON{
		ID: snapshot.ID, Kind: snapshot.Kind, State: snapshot.State, Percent: snapshot.Percent,
		Speed: snapshot.Speed, ETA: snapshot.ETA, DownloadedBytes: snapshot.DownloadedBytes,
		TotalBytes: snapshot.TotalBytes, FileName: snapshot.FileName, FileSize: snapshot.FileSize,
		TextPreview: snapshot.TextPreview, Error: snapshot.Error, CreatedAt: snapshot.CreatedAt,
	}
	if !snapshot.CompletedAt.IsZero() {
		completedAt := snapshot.CompletedAt
		value.CompletedAt = &completedAt
	}
	if !snapshot.DeleteAt.IsZero() {
		deleteAt := snapshot.DeleteAt
		value.DeleteAt = &deleteAt
	}
	return json.Marshal(value)
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
