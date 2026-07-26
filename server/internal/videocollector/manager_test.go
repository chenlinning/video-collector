package videocollector

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type engineStub struct{}

func (engineStub) Parse(context.Context, string) (*MediaInfo, error) {
	return &MediaInfo{ID: "media-1", Formats: []MediaFormat{{ID: "best", HasVideo: true}}}, nil
}

type blockingEngine struct {
	release chan struct{}
}

type transcriptEngineStub struct{}

func (transcriptEngineStub) Parse(context.Context, string) (*MediaInfo, error) {
	return &MediaInfo{ID: "media-1"}, nil
}

func (transcriptEngineStub) Download(_ context.Context, _ DownloadRequest, outputDir string, _ func(ProgressUpdate)) (*DownloadResult, error) {
	output := filepath.Join(outputDir, "output.srt")
	if err := os.WriteFile(output, []byte("1\n00:00:00,000 --> 00:00:01,000\nHello\n"), 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(outputDir, "output.txt"), []byte("Hello from transcription"), 0o600); err != nil {
		return nil, err
	}
	return &DownloadResult{Path: output, Extension: "srt"}, nil
}

func (engine blockingEngine) Parse(context.Context, string) (*MediaInfo, error) {
	return &MediaInfo{ID: "media-1"}, nil
}

func (engine blockingEngine) Download(ctx context.Context, _ DownloadRequest, outputDir string, _ func(ProgressUpdate)) (*DownloadResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-engine.release:
		path := filepath.Join(outputDir, "output.mp4")
		if err := os.WriteFile(path, []byte("video-data"), 0o600); err != nil {
			return nil, err
		}
		return &DownloadResult{Path: path, Extension: "mp4"}, nil
	}
}

func (engineStub) Download(_ context.Context, _ DownloadRequest, outputDir string, progress func(ProgressUpdate)) (*DownloadResult, error) {
	progress(ProgressUpdate{State: TaskStateDownloading, Percent: 50})
	path := filepath.Join(outputDir, "output.mp4")
	if err := os.WriteFile(path, []byte("video-data"), 0o600); err != nil {
		return nil, err
	}
	return &DownloadResult{Path: path, Extension: "mp4"}, nil
}

func waitForCompletedTask(t *testing.T, manager *Manager, taskID string) TaskSnapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := manager.Get(taskID)
		require.NoError(t, err)
		if task.State == TaskStateCompleted {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("task did not complete")
	return TaskSnapshot{}
}

func TestManagerDefaultsToFifteenMinuteDownloadAndThirtyMinuteUnclaimedRetention(t *testing.T) {
	manager, err := NewManager(ManagerConfig{
		Root:          t.TempDir(),
		MaxConcurrent: 1,
	}, engineStub{})
	require.NoError(t, err)
	defer manager.Close()

	require.Equal(t, 15*time.Minute, manager.downloadRetention)
	require.Equal(t, 30*time.Minute, manager.unclaimedRetention)
}

func TestManagerUsesRandomTaskCapabilityAndExpiresFifteenMinutesAfterDownload(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	manager, err := NewManager(ManagerConfig{
		Root:               t.TempDir(),
		DownloadRetention:  15 * time.Minute,
		UnclaimedRetention: 30 * time.Minute,
		MaxConcurrent:      1,
		Now:                func() time.Time { return now },
	}, engineStub{})
	require.NoError(t, err)

	task, err := manager.Start(DownloadRequest{
		SourceURL: "https://example.com/video",
		MediaID:   "media-1",
		Title:     "Test video",
		FormatID:  "best",
		HasAudio:  true,
	})
	require.NoError(t, err)
	require.Len(t, task.ID, 32)
	completed := waitForCompletedTask(t, manager, task.ID)
	require.NotEmpty(t, completed.FileName)

	_, err = manager.Get("00000000000000000000000000000000")
	require.ErrorIs(t, err, ErrTaskNotFound)

	lease, err := manager.OpenDownload(task.ID)
	require.NoError(t, err)
	require.Equal(t, now.Add(15*time.Minute), lease.DeleteAt)
	require.NoError(t, lease.Close())
	firstDeleteAt := lease.DeleteAt

	now = now.Add(5 * time.Minute)
	secondLease, err := manager.OpenDownload(task.ID)
	require.NoError(t, err)
	require.Equal(t, firstDeleteAt, secondLease.DeleteAt)
	require.NoError(t, secondLease.Close())

	now = firstDeleteAt.Add(-time.Second)
	manager.CleanupExpired()
	_, err = manager.Get(task.ID)
	require.NoError(t, err)

	now = firstDeleteAt
	manager.CleanupExpired()
	_, err = manager.Get(task.ID)
	require.ErrorIs(t, err, ErrTaskNotFound)
	require.NoDirExists(t, filepath.Join(manager.root, task.ID))
}

func TestManagerRemovesUnclaimedFiles(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	manager, err := NewManager(ManagerConfig{
		Root:               t.TempDir(),
		DownloadRetention:  15 * time.Minute,
		UnclaimedRetention: 30 * time.Minute,
		MaxConcurrent:      1,
		Now:                func() time.Time { return now },
	}, engineStub{})
	require.NoError(t, err)

	task, err := manager.Start(DownloadRequest{SourceURL: "https://example.com/video", FormatID: "best"})
	require.NoError(t, err)
	waitForCompletedTask(t, manager, task.ID)

	now = now.Add(30 * time.Minute)
	manager.CleanupExpired()
	_, err = manager.Get(task.ID)
	require.ErrorIs(t, err, ErrTaskNotFound)
}

func TestManagerRejectsTasksBeyondTheBoundedQueue(t *testing.T) {
	manager, err := NewManager(ManagerConfig{
		Root: t.TempDir(), MaxConcurrent: 1, MaxQueued: 1,
	}, blockingEngine{release: make(chan struct{})})
	require.NoError(t, err)
	defer manager.Close()

	request := DownloadRequest{SourceURL: "https://example.com/video", FormatID: "best"}
	_, err = manager.Start(request)
	require.NoError(t, err)
	_, err = manager.Start(request)
	require.NoError(t, err)
	_, err = manager.Start(request)
	require.ErrorIs(t, err, ErrTaskQueueFull)
}

func TestManagerTimesOutLongRunningTasks(t *testing.T) {
	manager, err := NewManager(ManagerConfig{
		Root: t.TempDir(), MaxConcurrent: 1, MaxQueued: 1, TaskTimeout: 20 * time.Millisecond,
	}, blockingEngine{release: make(chan struct{})})
	require.NoError(t, err)
	defer manager.Close()

	task, err := manager.Start(DownloadRequest{SourceURL: "https://example.com/video", FormatID: "best"})
	require.NoError(t, err)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snapshot, getErr := manager.Get(task.ID)
		require.NoError(t, getErr)
		if snapshot.State == TaskStateFailed {
			require.Contains(t, snapshot.Error, "deadline")
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("task did not time out")
}

func TestManagerAcceptsAllowedTranscriptionUploadAndRejectsUnsafeExtension(t *testing.T) {
	manager, err := NewManager(ManagerConfig{Root: t.TempDir(), MaxConcurrent: 1}, engineStub{})
	require.NoError(t, err)
	defer manager.Close()

	task, err := manager.StartUpload("sample.mp3", bytes.NewBufferString("audio-data"))
	require.NoError(t, err)
	completed := waitForCompletedTask(t, manager, task.ID)
	require.Equal(t, TaskKindTranscript, completed.Kind)

	_, err = manager.StartUpload("payload.html", bytes.NewBufferString("not media"))
	require.ErrorIs(t, err, ErrInvalidDownload)
}

func TestManagerReturnsTranscriptTextPreview(t *testing.T) {
	manager, err := NewManager(ManagerConfig{Root: t.TempDir(), MaxConcurrent: 1}, transcriptEngineStub{})
	require.NoError(t, err)
	defer manager.Close()

	task, err := manager.StartUpload("sample.mp3", bytes.NewBufferString("audio-data"))
	require.NoError(t, err)
	completed := waitForCompletedTask(t, manager, task.ID)
	require.Equal(t, "Hello from transcription", completed.TextPreview)
}
