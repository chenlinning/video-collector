package videocollector

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	maxMetadataBytes      = 16 * 1024 * 1024
	maxExtractorAttempts  = 3
	extractorRetryBackoff = 500 * time.Millisecond
)

var (
	formatIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$`)
	urlInError      = regexp.MustCompile(`https?://\S+`)
)

type YTDLPEngine struct {
	ytDLPPath  string
	ffmpegPath string
	resolver   IPResolver
}

func NewYTDLPEngine(ytDLPPath, ffmpegPath string, resolver IPResolver) *YTDLPEngine {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &YTDLPEngine{ytDLPPath: ytDLPPath, ffmpegPath: ffmpegPath, resolver: resolver}
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

func (e *YTDLPEngine) Download(ctx context.Context, request DownloadRequest, outputDir string, progress func(ProgressUpdate)) (*DownloadResult, error) {
	parsed, err := ValidatePublicMediaURL(ctx, request.SourceURL, e.resolver)
	if err != nil {
		return nil, err
	}
	if !formatIDPattern.MatchString(request.FormatID) {
		return nil, ErrInvalidDownload
	}
	request.SourceURL = parsed.String()
	result, stderr, err := retryExtractor(ctx, maxExtractorAttempts, extractorRetryBackoff, func() (*DownloadResult, string, error) {
		return e.downloadOnce(ctx, request, outputDir, progress)
	})
	if err != nil {
		return nil, extractorError(stderr, err)
	}
	return result, nil
}

func (e *YTDLPEngine) downloadOnce(ctx context.Context, request DownloadRequest, outputDir string, progress func(ProgressUpdate)) (*DownloadResult, string, error) {
	formatExpression := request.FormatID
	if !request.HasAudio {
		formatExpression += "+bestaudio/best"
	}
	outputTemplate := filepath.Join(outputDir, "output.%(ext)s")
	args := []string{
		"--newline",
		"--continue",
		"--no-playlist",
		"--no-warnings",
		"--no-simulate",
		"--max-filesize", "2G",
		"--ffmpeg-location", e.ffmpegPath,
		"-f", formatExpression,
		"--merge-output-format", "mp4",
		"--progress-template", "download:__VC_PROGRESS__:%(progress._percent_str)s|%(progress._speed_str)s|%(progress._eta_str)s|%(progress.downloaded_bytes)s|%(progress.total_bytes_estimate)s",
		"--print", "before_dl:__VC_PROCESSING__:preparing",
		"--print", "post_process:__VC_PROCESSING__:processing",
		"--print", "after_move:__VC_DONE__:%(filepath)s",
		"-o", outputTemplate,
		"--", request.SourceURL,
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
