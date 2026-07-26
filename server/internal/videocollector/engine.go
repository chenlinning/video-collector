package videocollector

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxMetadataBytes      = 16 * 1024 * 1024
	maxImageBytes         = 50 * 1024 * 1024
	maxExtractorAttempts  = 3
	extractorRetryBackoff = 500 * time.Millisecond
)

var (
	formatIDPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$`)
	resourceIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	urlInError        = regexp.MustCompile(`https?://\S+`)
)

type YTDLPEngine struct {
	ytDLPPath   string
	ffmpegPath  string
	whisperPath string
	modelPath   string
	resolver    IPResolver
}

func NewYTDLPEngine(ytDLPPath, ffmpegPath string, resolver IPResolver) *YTDLPEngine {
	return NewYTDLPEngineWithTranscriber(ytDLPPath, ffmpegPath, "", "", resolver)
}

func NewYTDLPEngineWithTranscriber(ytDLPPath, ffmpegPath, whisperPath, modelPath string, resolver IPResolver) *YTDLPEngine {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &YTDLPEngine{
		ytDLPPath: ytDLPPath, ffmpegPath: ffmpegPath, whisperPath: whisperPath, modelPath: modelPath, resolver: resolver,
	}
}

func (e *YTDLPEngine) Parse(ctx context.Context, sourceURL string) (*MediaInfo, error) {
	parsed, err := ValidatePublicMediaURL(ctx, sourceURL, e.resolver)
	if err != nil {
		return nil, err
	}
	stdout, stderr, err := retryExtractor(ctx, maxExtractorAttempts, extractorRetryBackoff, func() ([]byte, string, error) {
		return runCaptured(ctx, e.ytDLPPath, []string{
			"--no-playlist",
			"--skip-download",
			"--dump-single-json",
			"--no-warnings",
			"--ffmpeg-location", e.ffmpegPath,
			"--", parsed.String(),
		})
	})
	if err != nil {
		return nil, extractorError(stderr, err)
	}
	return NormalizeMediaInfo(json.RawMessage(stdout), parsed.String())
}

func (e *YTDLPEngine) ParseCollection(ctx context.Context, sourceURL string, limit int) (*CollectionInfo, error) {
	parsed, err := ValidatePublicMediaURL(ctx, sourceURL, e.resolver)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 10 {
		limit = 10
	}
	stdout, stderr, err := retryExtractor(ctx, maxExtractorAttempts, extractorRetryBackoff, func() ([]byte, string, error) {
		return runCaptured(ctx, e.ytDLPPath, []string{
			"--flat-playlist",
			"--playlist-end", strconv.Itoa(limit),
			"--dump-single-json",
			"--no-warnings",
			"--", parsed.String(),
		})
	})
	if err != nil {
		return nil, extractorError(stderr, err)
	}
	return NormalizeCollectionInfo(json.RawMessage(stdout), parsed.String(), limit)
}

func (e *YTDLPEngine) Download(ctx context.Context, request DownloadRequest, outputDir string, progress func(ProgressUpdate)) (*DownloadResult, error) {
	kind := request.Kind
	if kind == "" {
		kind = TaskKindMedia
	}
	request.Kind = kind
	if kind == TaskKindTranscript && request.InputPath != "" {
		if !pathWithin(outputDir, request.InputPath) {
			return nil, ErrInvalidDownload
		}
		return e.transcribeInput(ctx, request.InputPath, outputDir, progress)
	}
	parsed, err := ValidatePublicMediaURL(ctx, request.SourceURL, e.resolver)
	if err != nil {
		return nil, err
	}
	if kind == TaskKindMedia && !formatIDPattern.MatchString(request.FormatID) {
		return nil, ErrInvalidDownload
	}
	request.SourceURL = parsed.String()
	if kind == TaskKindImage {
		return e.downloadImage(ctx, request, outputDir)
	}
	if kind == TaskKindTranscript {
		return e.transcribeURL(ctx, request.SourceURL, outputDir, progress)
	}
	result, stderr, err := retryExtractor(ctx, maxExtractorAttempts, extractorRetryBackoff, func() (*DownloadResult, string, error) {
		return e.downloadOnce(ctx, request, outputDir, progress)
	})
	if err != nil {
		return nil, extractorError(stderr, err)
	}
	return result, nil
}

func (e *YTDLPEngine) transcribeURL(ctx context.Context, sourceURL, outputDir string, progress func(ProgressUpdate)) (*DownloadResult, error) {
	if e.whisperPath == "" || e.modelPath == "" {
		return nil, errors.New("transcription runtime is unavailable")
	}
	if progress != nil {
		progress(ProgressUpdate{State: TaskStateDownloading, Percent: 5})
	}
	args := []string{
		"--no-playlist", "--no-warnings", "--max-filesize", "250M",
		"-f", "bestaudio/best", "-x", "--audio-format", "wav",
		"--ffmpeg-location", e.ffmpegPath,
		"-o", filepath.Join(outputDir, "input.%(ext)s"), "--", sourceURL,
	}
	if output, err := exec.CommandContext(ctx, e.ytDLPPath, args...).CombinedOutput(); err != nil {
		return nil, extractorError(string(output), err)
	}
	inputPath := findInputFile(outputDir)
	if inputPath == "" {
		return nil, errors.New("transcription audio input is missing")
	}
	return e.transcribeInput(ctx, inputPath, outputDir, progress)
}

func (e *YTDLPEngine) transcribeInput(ctx context.Context, inputPath, outputDir string, progress func(ProgressUpdate)) (*DownloadResult, error) {
	if e.whisperPath == "" || e.modelPath == "" || !pathWithin(outputDir, inputPath) {
		return nil, errors.New("transcription runtime is unavailable")
	}
	if err := ensureTranscriptionDuration(ctx, e.ffmpegPath, inputPath); err != nil {
		return nil, err
	}
	ffmpegArgs, whisperArgs, err := buildTranscriptionCommands(inputPath, outputDir, e.ffmpegPath, e.whisperPath, e.modelPath)
	if err != nil {
		return nil, err
	}
	if progress != nil {
		progress(ProgressUpdate{State: TaskStateProcessing, Percent: 30})
	}
	if output, err := exec.CommandContext(ctx, e.ffmpegPath, ffmpegArgs...).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("prepare transcription audio: %s", safeCommandOutput(output, err))
	}
	if progress != nil {
		progress(ProgressUpdate{State: TaskStateProcessing, Percent: 60})
	}
	if output, err := exec.CommandContext(ctx, e.whisperPath, whisperArgs...).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("transcribe media: %s", safeCommandOutput(output, err))
	}
	outputPath := filepath.Join(outputDir, "output.srt")
	if !isRegularOutputFile(outputDir, outputPath) {
		return nil, errors.New("transcription output is missing")
	}
	return &DownloadResult{Path: outputPath, Extension: "srt"}, nil
}

func ensureTranscriptionDuration(ctx context.Context, ffmpegPath, inputPath string) error {
	probePath := filepath.Join(filepath.Dir(ffmpegPath), "ffprobe"+filepath.Ext(ffmpegPath))
	output, err := exec.CommandContext(ctx, probePath,
		"-v", "error", "-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1", "--", inputPath,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("inspect transcription media: %s", safeCommandOutput(output, err))
	}
	_, err = parseProbeDuration(string(output))
	return err
}

func parseProbeDuration(output string) (float64, error) {
	duration, err := strconv.ParseFloat(strings.TrimSpace(output), 64)
	if err != nil || duration < 0 {
		return 0, errors.New("unable to determine media duration")
	}
	if duration > 3600 {
		return 0, ErrMediaTooLong
	}
	return duration, nil
}

func buildTranscriptionCommands(inputPath, outputDir, ffmpegPath, whisperPath, modelPath string) ([]string, []string, error) {
	if inputPath == "" || outputDir == "" || ffmpegPath == "" || whisperPath == "" || modelPath == "" {
		return nil, nil, ErrInvalidDownload
	}
	wavPath := filepath.Join(outputDir, "transcription.wav")
	ffmpegArgs := []string{
		"-hide_banner", "-loglevel", "error", "-y", "-i", inputPath,
		"-t", "3600", "-vn", "-ar", "16000", "-ac", "1", "-c:a", "pcm_s16le", wavPath,
	}
	whisperArgs := []string{
		"-m", modelPath, "-f", wavPath, "-l", "auto", "-osrt", "-otxt",
		"-of", filepath.Join(outputDir, "output"), "--no-prints",
	}
	return ffmpegArgs, whisperArgs, nil
}

func findInputFile(outputDir string) string {
	matches, _ := filepath.Glob(filepath.Join(outputDir, "input.*"))
	for _, match := range matches {
		if isRegularOutputFile(outputDir, match) {
			return match
		}
	}
	return ""
}

func safeCommandOutput(output []byte, fallback error) string {
	message := strings.TrimSpace(string(output))
	if message == "" && fallback != nil {
		message = fallback.Error()
	}
	message = urlInError.ReplaceAllString(message, "[url]")
	if len(message) > 500 {
		message = message[len(message)-500:]
	}
	return message
}

func (e *YTDLPEngine) downloadImage(ctx context.Context, request DownloadRequest, outputDir string) (*DownloadResult, error) {
	media, err := e.Parse(ctx, request.SourceURL)
	if err != nil {
		return nil, err
	}
	image, err := selectMediaImage(media, request.ResourceID)
	if err != nil {
		return nil, err
	}
	parsed, err := ValidatePublicMediaURL(ctx, image.URL, e.resolver)
	if err != nil {
		return nil, err
	}
	client := newSafeHTTPClient(e.resolver)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("User-Agent", "VideoCollector/1.0")
	response, err := client.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("image download failed with status %d", response.StatusCode)
	}
	extension := safeImageExtension(response.Header.Get("Content-Type"), response.Request.URL.String())
	if extension == "" {
		return nil, ErrInvalidDownload
	}
	if response.ContentLength > maxImageBytes {
		return nil, errors.New("image exceeds the 50 MiB limit")
	}
	outputPath := filepath.Join(outputDir, "output."+extension)
	output, err := os.OpenFile(outputPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	written, copyErr := io.Copy(output, io.LimitReader(response.Body, maxImageBytes+1))
	closeErr := output.Close()
	if copyErr != nil {
		return nil, copyErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if written > maxImageBytes {
		_ = os.Remove(outputPath)
		return nil, errors.New("image exceeds the 50 MiB limit")
	}
	return &DownloadResult{Path: outputPath, Extension: extension}, nil
}

func selectMediaImage(media *MediaInfo, imageID string) (MediaImage, error) {
	if media == nil || strings.TrimSpace(imageID) == "" {
		return MediaImage{}, ErrInvalidDownload
	}
	for _, image := range media.Images {
		if image.ID == imageID && strings.TrimSpace(image.URL) != "" {
			return image, nil
		}
	}
	return MediaImage{}, ErrInvalidDownload
}

func safeImageExtension(contentType, rawURL string) string {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	byType := map[string]string{
		"image/jpeg": "jpg", "image/png": "png", "image/webp": "webp",
		"image/gif": "gif", "image/avif": "avif",
	}
	if extension := byType[mediaType]; extension != "" {
		return extension
	}
	if mediaType != "" && mediaType != "application/octet-stream" {
		return ""
	}
	extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(strings.SplitN(rawURL, "?", 2)[0])), ".")
	for _, allowed := range []string{"jpg", "jpeg", "png", "webp", "gif", "avif"} {
		if extension == allowed {
			if extension == "jpeg" {
				return "jpg"
			}
			return extension
		}
	}
	return ""
}

func newSafeHTTPClient(resolver IPResolver) *http.Client {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, ErrUnsafeMediaURL
			}
			addresses, err := resolver.LookupIPAddr(ctx, host)
			if err != nil || len(addresses) == 0 {
				return nil, ErrUnsafeMediaURL
			}
			for _, address := range addresses {
				if !isPublicIP(address.IP) {
					return nil, ErrUnsafeMediaURL
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].IP.String(), port))
		},
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   2 * time.Minute,
		CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			_, err := ValidatePublicMediaURL(request.Context(), request.URL.String(), resolver)
			return err
		},
	}
}

func (e *YTDLPEngine) downloadOnce(ctx context.Context, request DownloadRequest, outputDir string, progress func(ProgressUpdate)) (*DownloadResult, string, error) {
	args, err := buildDownloadArgs(request, outputDir, e.ffmpegPath)
	if err != nil {
		return nil, "", err
	}
	cmd := exec.CommandContext(ctx, e.ytDLPPath, args...)
	configureCommandCancellation(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, "", err
	}
	if err := cmd.Start(); err != nil {
		return nil, "", err
	}

	var outputPath string
	var errorTail tailBuffer
	var outputMu sync.Mutex
	readLines := func(reader io.Reader, captureErrors bool) {
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if update, ok := ParseProgressLine(line); ok && progress != nil {
				progress(update)
			}
			if strings.HasPrefix(strings.TrimSpace(line), "__VC_DONE__:") {
				outputMu.Lock()
				outputPath = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "__VC_DONE__:"))
				outputMu.Unlock()
			}
			if captureErrors {
				_, _ = errorTail.Write([]byte(line + "\n"))
			}
		}
	}
	var readers sync.WaitGroup
	readers.Add(2)
	go func() { defer readers.Done(); readLines(stdout, false) }()
	go func() { defer readers.Done(); readLines(stderr, true) }()
	waitErr := cmd.Wait()
	readers.Wait()
	if waitErr != nil {
		return nil, errorTail.String(), waitErr
	}
	outputMu.Lock()
	resolvedPath := outputPath
	outputMu.Unlock()
	if !isRegularOutputFile(outputDir, resolvedPath) {
		resolvedPath = findOutputFile(outputDir)
	}
	if resolvedPath == "" {
		return nil, errorTail.String(), errors.New("download completed without an output file")
	}
	return &DownloadResult{Path: resolvedPath, Extension: strings.TrimPrefix(filepath.Ext(resolvedPath), ".")}, errorTail.String(), nil
}

func buildDownloadArgs(request DownloadRequest, outputDir, ffmpegPath string) ([]string, error) {
	kind := request.Kind
	if kind == "" {
		kind = TaskKindMedia
	}
	args := []string{
		"--newline",
		"--continue",
		"--no-playlist",
		"--no-warnings",
		"--no-simulate",
		"--max-filesize", "2G",
		"--ffmpeg-location", ffmpegPath,
		"--progress-template", "download:__VC_PROGRESS__:%(progress._percent_str)s|%(progress._speed_str)s|%(progress._eta_str)s|%(progress.downloaded_bytes)s|%(progress.total_bytes_estimate)s",
		"--print", "before_dl:__VC_PROCESSING__:preparing",
		"--print", "post_process:__VC_PROCESSING__:processing",
		"--print", "after_move:__VC_DONE__:%(filepath)s",
	}
	switch kind {
	case TaskKindMedia:
		if !formatIDPattern.MatchString(request.FormatID) {
			return nil, ErrInvalidDownload
		}
		formatExpression := request.FormatID
		if !request.HasAudio {
			formatExpression += "+bestaudio/best"
		}
		args = append(args, "-f", formatExpression, "--merge-output-format", "mp4")
	case TaskKindAudio:
		args = append(args, "-f", "bestaudio/best", "--extract-audio", "--audio-format", "mp3", "--audio-quality", "0")
	case TaskKindSubtitle:
		if !resourceIDPattern.MatchString(request.ResourceID) {
			return nil, ErrInvalidDownload
		}
		writeFlag := "--write-subs"
		if request.Automatic {
			writeFlag = "--write-auto-subs"
		}
		args = append(args, "--skip-download", writeFlag, "--sub-langs", request.ResourceID, "--sub-format", "best", "--convert-subs", "srt")
	default:
		return nil, ErrInvalidDownload
	}
	args = append(args, "-o", filepath.Join(outputDir, "output.%(ext)s"), "--", request.SourceURL)
	return args, nil
}

func retryExtractor[T any](ctx context.Context, attempts int, backoff time.Duration, run func() (T, string, error)) (T, string, error) {
	if attempts < 1 {
		attempts = 1
	}
	for attempt := 1; ; attempt++ {
		value, stderr, err := run()
		if err == nil || attempt >= attempts || !isTransientExtractorFailure(stderr, err) {
			return value, stderr, err
		}
		delay := time.Duration(attempt) * backoff
		if delay <= 0 {
			continue
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			var zero T
			return zero, stderr, ctx.Err()
		case <-timer.C:
		}
	}
}

func isTransientExtractorFailure(stderr string, err error) bool {
	message := strings.ToLower(stderr)
	if err != nil {
		message += "\n" + strings.ToLower(err.Error())
	}
	for _, marker := range []string{
		"unexpected response from webpage request",
		"unable to extract challenge data",
		"unable to extract universal data for rehydration",
		"http error 403",
		"http error 408",
		"http error 429",
		"http error 500",
		"http error 502",
		"http error 503",
		"http error 504",
		"connection reset",
		"remote end closed connection",
		"temporary failure in name resolution",
		"timed out",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func findOutputFile(outputDir string) string {
	matches, _ := filepath.Glob(filepath.Join(outputDir, "output.*"))
	for _, match := range matches {
		if isRegularOutputFile(outputDir, match) {
			return match
		}
	}
	return ""
}

func isRegularOutputFile(outputDir, candidate string) bool {
	if candidate == "" || strings.HasSuffix(candidate, ".part") || !pathWithin(outputDir, candidate) {
		return false
	}
	info, err := os.Stat(candidate)
	return err == nil && info.Mode().IsRegular()
}

type limitedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
}

func (w *limitedBuffer) Write(p []byte) (int, error) {
	if w.buffer.Len() < w.limit {
		remaining := w.limit - w.buffer.Len()
		if len(p) > remaining {
			_, _ = w.buffer.Write(p[:remaining])
			w.overflow = true
		} else {
			_, _ = w.buffer.Write(p)
		}
	} else {
		w.overflow = true
	}
	return len(p), nil
}

type tailBuffer struct {
	mu     sync.Mutex
	buffer []byte
}

func (w *tailBuffer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buffer = append(w.buffer, p...)
	if len(w.buffer) > 32*1024 {
		w.buffer = append([]byte(nil), w.buffer[len(w.buffer)-32*1024:]...)
	}
	return len(p), nil
}

func (w *tailBuffer) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.buffer)
}

func runCaptured(ctx context.Context, executable string, args []string) ([]byte, string, error) {
	cmd := exec.CommandContext(ctx, executable, args...)
	configureCommandCancellation(cmd)
	stdout := &limitedBuffer{limit: maxMetadataBytes}
	stderr := &tailBuffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if stdout.overflow {
		return nil, stderr.String(), errors.New("media metadata exceeded the size limit")
	}
	return stdout.buffer.Bytes(), stderr.String(), err
}

func extractorError(stderr string, fallback error) error {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "ERROR:") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "ERROR:"))
			line = urlInError.ReplaceAllString(line, "[url]")
			if len(line) > 500 {
				line = line[:500]
			}
			return errors.New(line)
		}
	}
	return fmt.Errorf("media extraction failed: %w", fallback)
}

func EnsureRuntimeFiles(paths ...string) error {
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			return err
		}
	}
	return nil
}
