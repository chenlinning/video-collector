package videocollector

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	ErrTaskNotFound    = errors.New("video task not found")
	ErrTaskNotReady    = errors.New("video task is not ready for download")
	ErrTaskQueueFull   = errors.New("video task queue is full")
	ErrInvalidDownload = errors.New("invalid video download request")
	ErrMediaTooLong    = errors.New("media duration exceeds the 60 minute limit")
)

var unsafeFileNameChars = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]+`)

const (
	DefaultDownloadRetention  = 15 * time.Minute
	DefaultUnclaimedRetention = 30 * time.Minute
	MaxUploadBytes            = 250 * 1024 * 1024
)

type ManagerConfig struct {
	Root               string
	DownloadRetention  time.Duration
	UnclaimedRetention time.Duration
	MaxConcurrent      int
	MaxQueued          int
	TaskTimeout        time.Duration
	Now                func() time.Time
}

type task struct {
	snapshot        TaskSnapshot
	request         DownloadRequest
	cancel          context.CancelFunc
	filePath        string
	activeDownloads int
	expired         bool
}

type Manager struct {
	mu                 sync.Mutex
	tasks              map[string]*task
	root               string
	downloadRetention  time.Duration
	unclaimedRetention time.Duration
	now                func() time.Time
	engine             Engine
	semaphore          chan struct{}
	maxOutstanding     int
	taskTimeout        time.Duration
}

func NewManager(config ManagerConfig, engine Engine) (*Manager, error) {
	if engine == nil || strings.TrimSpace(config.Root) == "" {
		return nil, ErrInvalidDownload
	}
	if config.DownloadRetention <= 0 {
		config.DownloadRetention = DefaultDownloadRetention
	}
	if config.UnclaimedRetention <= 0 {
		config.UnclaimedRetention = DefaultUnclaimedRetention
	}
	if config.MaxConcurrent <= 0 {
		config.MaxConcurrent = 2
	}
	if config.MaxQueued <= 0 {
		config.MaxQueued = config.MaxConcurrent * 2
	}
	if config.TaskTimeout <= 0 {
		config.TaskTimeout = 30 * time.Minute
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if err := os.MkdirAll(config.Root, 0o700); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(config.Root)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(config.Root, entry.Name())); err != nil {
			return nil, err
		}
	}
	return &Manager{
		tasks:              make(map[string]*task),
		root:               config.Root,
		downloadRetention:  config.DownloadRetention,
		unclaimedRetention: config.UnclaimedRetention,
		now:                config.Now,
		engine:             engine,
		semaphore:          make(chan struct{}, config.MaxConcurrent),
		maxOutstanding:     config.MaxConcurrent + config.MaxQueued,
		taskTimeout:        config.TaskTimeout,
	}, nil
}

func (m *Manager) Parse(ctx context.Context, sourceURL string) (*MediaInfo, error) {
	select {
	case m.semaphore <- struct{}{}:
		defer func() { <-m.semaphore }()
		return m.engine.Parse(ctx, sourceURL)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *Manager) ParseCollection(ctx context.Context, sourceURL string, limit int) (*CollectionInfo, error) {
	engine, ok := m.engine.(interface {
		ParseCollection(context.Context, string, int) (*CollectionInfo, error)
	})
	if !ok {
		return nil, ErrInvalidDownload
	}
	select {
	case m.semaphore <- struct{}{}:
		defer func() { <-m.semaphore }()
		return engine.ParseCollection(ctx, sourceURL, limit)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *Manager) Start(request DownloadRequest) (TaskSnapshot, error) {
	if !validDownloadRequest(&request) {
		return TaskSnapshot{}, ErrInvalidDownload
	}
	return m.startPrepared(request, nil)
}

func (m *Manager) StartUpload(fileName string, reader io.Reader) (TaskSnapshot, error) {
	extension := safeUploadExtension(fileName)
	if extension == "" || reader == nil {
		return TaskSnapshot{}, ErrInvalidDownload
	}
	title := strings.TrimSuffix(filepath.Base(fileName), filepath.Ext(fileName))
	request := DownloadRequest{Kind: TaskKindTranscript, SourceURL: "upload://local", Title: title}
	return m.startPrepared(request, func(directory string, prepared *DownloadRequest) error {
		inputPath := filepath.Join(directory, "input."+extension)
		output, err := os.OpenFile(inputPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		written, copyErr := io.Copy(output, io.LimitReader(reader, MaxUploadBytes+1))
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if written > MaxUploadBytes {
			return errors.New("upload exceeds the 250 MiB limit")
		}
		prepared.InputPath = inputPath
		return nil
	})
}

func (m *Manager) startPrepared(request DownloadRequest, prepare func(string, *DownloadRequest) error) (TaskSnapshot, error) {
	taskID, err := randomTaskID()
	if err != nil {
		return TaskSnapshot{}, err
	}
	directory := filepath.Join(m.root, taskID)
	if err := os.Mkdir(directory, 0o700); err != nil {
		return TaskSnapshot{}, err
	}
	if prepare != nil {
		if err := prepare(directory, &request); err != nil {
			_ = os.RemoveAll(directory)
			return TaskSnapshot{}, err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), m.taskTimeout)
	now := m.now()
	item := &task{
		request: request,
		cancel:  cancel,
		snapshot: TaskSnapshot{
			ID:        taskID,
			Kind:      request.Kind,
			State:     TaskStateQueued,
			CreatedAt: now,
		},
	}

	m.mu.Lock()
	activeTasks := 0
	for _, existing := range m.tasks {
		if isActiveTaskState(existing.snapshot.State) {
			activeTasks++
		}
	}
	if activeTasks >= m.maxOutstanding {
		m.mu.Unlock()
		cancel()
		_ = os.RemoveAll(directory)
		return TaskSnapshot{}, ErrTaskQueueFull
	}
	m.tasks[taskID] = item
	initialSnapshot := item.snapshot
	m.mu.Unlock()

	go m.runTask(ctx, item, directory)
	return initialSnapshot, nil
}

func safeUploadExtension(fileName string) string {
	extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(strings.TrimSpace(fileName))), ".")
	for _, allowed := range []string{"mp3", "m4a", "aac", "wav", "flac", "ogg", "opus", "mp4", "mov", "mkv", "webm"} {
		if extension == allowed {
			return extension
		}
	}
	return ""
}

func validDownloadRequest(request *DownloadRequest) bool {
	if strings.TrimSpace(request.SourceURL) == "" {
		return false
	}
	if request.Kind == "" {
		request.Kind = TaskKindMedia
	}
	switch request.Kind {
	case TaskKindMedia:
		return strings.TrimSpace(request.FormatID) != ""
	case TaskKindAudio:
		return true
	case TaskKindImage, TaskKindSubtitle:
		return strings.TrimSpace(request.ResourceID) != ""
	case TaskKindTranscript:
		return strings.TrimSpace(request.SourceURL) != "" || strings.TrimSpace(request.InputPath) != ""
	default:
		return false
	}
}

func (m *Manager) Get(taskID string) (TaskSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.tasks[taskID]
	if !ok {
		return TaskSnapshot{}, ErrTaskNotFound
	}
	return item.snapshot, nil
}

func (m *Manager) Cancel(taskID string) error {
	m.mu.Lock()
	item, ok := m.tasks[taskID]
	if !ok {
		m.mu.Unlock()
		return ErrTaskNotFound
	}
	if !isActiveTaskState(item.snapshot.State) {
		m.mu.Unlock()
		return ErrTaskNotReady
	}
	item.snapshot.State = TaskStateCancelled
	item.snapshot.CompletedAt = m.now()
	cancel := item.cancel
	m.mu.Unlock()
	cancel()
	return nil
}

type DownloadLease struct {
	*os.File
	FileName string
	FileSize int64
	DeleteAt time.Time
	manager  *Manager
	taskID   string
	once     sync.Once
}

func (l *DownloadLease) Close() error {
	var closeErr error
	l.once.Do(func() {
		closeErr = l.File.Close()
		l.manager.releaseDownload(l.taskID)
	})
	return closeErr
}

func (m *Manager) OpenDownload(taskID string) (*DownloadLease, error) {
	m.mu.Lock()
	item, ok := m.tasks[taskID]
	if !ok {
		m.mu.Unlock()
		return nil, ErrTaskNotFound
	}
	now := m.now()
	if item.snapshot.State != TaskStateCompleted || item.filePath == "" || item.expired || (!item.snapshot.DeleteAt.IsZero() && !now.Before(item.snapshot.DeleteAt)) {
		m.mu.Unlock()
		return nil, ErrTaskNotReady
	}
	file, err := os.Open(item.filePath)
	if err != nil {
		m.mu.Unlock()
		return nil, ErrTaskNotReady
	}
	if item.snapshot.DeleteAt.IsZero() {
		item.snapshot.DeleteAt = now.Add(m.downloadRetention)
	}
	item.activeDownloads++
	lease := &DownloadLease{
		File:     file,
		FileName: item.snapshot.FileName,
		FileSize: item.snapshot.FileSize,
		DeleteAt: item.snapshot.DeleteAt,
		manager:  m,
		taskID:   taskID,
	}
	m.mu.Unlock()
	return lease, nil
}

func (m *Manager) CleanupExpired() {
	now := m.now()
	var directories []string
	m.mu.Lock()
	for id, item := range m.tasks {
		expiresAt := item.snapshot.DeleteAt
		if expiresAt.IsZero() && !item.snapshot.CompletedAt.IsZero() {
			expiresAt = item.snapshot.CompletedAt.Add(m.unclaimedRetention)
		}
		if expiresAt.IsZero() || now.Before(expiresAt) {
			continue
		}
		item.expired = true
		item.snapshot.State = TaskStateExpired
		if item.activeDownloads == 0 {
			delete(m.tasks, id)
			directories = append(directories, filepath.Join(m.root, id))
		}
	}
	m.mu.Unlock()
	for _, directory := range directories {
		_ = os.RemoveAll(directory)
	}
}

func (m *Manager) Close() {
	m.mu.Lock()
	for _, item := range m.tasks {
		item.cancel()
	}
	m.mu.Unlock()
}

func (m *Manager) runTask(ctx context.Context, item *task, directory string) {
	select {
	case m.semaphore <- struct{}{}:
		defer func() { <-m.semaphore }()
	case <-ctx.Done():
		m.finishCancelled(item, directory)
		return
	}

	result, err := m.engine.Download(ctx, item.request, directory, func(update ProgressUpdate) {
		m.updateProgress(item.snapshot.ID, update)
	})
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			m.finishCancelled(item, directory)
			return
		}
		m.finishFailed(item, directory, err)
		return
	}
	if result == nil || !pathWithin(directory, result.Path) {
		m.finishFailed(item, directory, errors.New("media extractor returned an invalid output path"))
		return
	}
	fileInfo, err := os.Stat(result.Path)
	if err != nil || !fileInfo.Mode().IsRegular() {
		m.finishFailed(item, directory, errors.New("download output file is missing"))
		return
	}
	textPreview := ""
	if item.request.Kind == TaskKindTranscript {
		textPreview = readTextPreview(filepath.Join(directory, "output.txt"))
	}

	m.mu.Lock()
	current, ok := m.tasks[item.snapshot.ID]
	if ok && current.snapshot.State != TaskStateCancelled {
		current.filePath = result.Path
		current.snapshot.State = TaskStateCompleted
		current.snapshot.Percent = 100
		current.snapshot.FileName = downloadFileName(current.request.Title, result.Extension)
		current.snapshot.FileSize = fileInfo.Size()
		current.snapshot.TextPreview = textPreview
		current.snapshot.CompletedAt = m.now()
	}
	m.mu.Unlock()
}

func readTextPreview(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, 32*1024))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(content))
}

func (m *Manager) updateProgress(taskID string, update ProgressUpdate) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.tasks[taskID]
	if !ok || item.snapshot.State == TaskStateCancelled {
		return
	}
	item.snapshot.State = update.State
	item.snapshot.Percent = update.Percent
	item.snapshot.Speed = update.Speed
	item.snapshot.ETA = update.ETA
	item.snapshot.DownloadedBytes = update.DownloadedBytes
	item.snapshot.TotalBytes = update.TotalBytes
}

func (m *Manager) finishCancelled(item *task, directory string) {
	m.mu.Lock()
	if current, ok := m.tasks[item.snapshot.ID]; ok {
		current.snapshot.State = TaskStateCancelled
		current.snapshot.CompletedAt = m.now()
	}
	m.mu.Unlock()
	_ = os.RemoveAll(directory)
}

func (m *Manager) finishFailed(item *task, directory string, taskErr error) {
	m.mu.Lock()
	if current, ok := m.tasks[item.snapshot.ID]; ok {
		current.snapshot.State = TaskStateFailed
		current.snapshot.Error = safeTaskError(taskErr)
		current.snapshot.CompletedAt = m.now()
	}
	m.mu.Unlock()
	_ = os.RemoveAll(directory)
}

func (m *Manager) releaseDownload(taskID string) {
	var directory string
	m.mu.Lock()
	if item, ok := m.tasks[taskID]; ok {
		if item.activeDownloads > 0 {
			item.activeDownloads--
		}
		if item.expired && item.activeDownloads == 0 {
			delete(m.tasks, taskID)
			directory = filepath.Join(m.root, taskID)
		}
	}
	m.mu.Unlock()
	if directory != "" {
		_ = os.RemoveAll(directory)
	}
}

func randomTaskID() (string, error) {
	value := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func isActiveTaskState(state TaskState) bool {
	return state == TaskStateQueued || state == TaskStateDownloading || state == TaskStateProcessing
}

func pathWithin(root, candidate string) bool {
	rootPath, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	candidatePath, err := filepath.Abs(candidate)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(rootPath, candidatePath)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func downloadFileName(title, extension string) string {
	name := strings.TrimSpace(unsafeFileNameChars.ReplaceAllString(title, "_"))
	name = strings.Trim(name, " .")
	if name == "" {
		name = "video"
	}
	runes := []rune(name)
	if len(runes) > 100 {
		name = string(runes[:100])
	}
	extension = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(extension)), ".")
	if extension == "" || len(extension) > 10 {
		extension = "mp4"
	}
	return fmt.Sprintf("%s.%s", name, extension)
}

func safeTaskError(err error) string {
	if err == nil {
		return "download failed"
	}
	message := strings.TrimSpace(err.Error())
	if len(message) > 500 {
		message = message[:500]
	}
	return message
}
