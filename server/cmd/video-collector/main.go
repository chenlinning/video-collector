package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/chenlinning/video-collector/server/internal/videocollector"
	"github.com/chenlinning/video-collector/server/internal/webapp"
)

func main() {
	config, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	if err := videocollector.EnsureRuntimeFiles(config.ytDLPPath, config.ffmpegPath, config.whisperPath, config.whisperModelPath); err != nil {
		log.Fatalf("video collector runtime unavailable: %v", err)
	}
	egress, err := videocollector.NewEgressRouter(config.egress)
	if err != nil {
		log.Fatalf("initialize egress router: %v", err)
	}

	engine := videocollector.NewYTDLPEngineWithTranscriberAndEgress(
		config.ytDLPPath, config.ffmpegPath, config.whisperPath, config.whisperModelPath, nil, egress,
	)
	manager, err := videocollector.NewManager(videocollector.ManagerConfig{
		Root:               config.tempRoot,
		DownloadRetention:  videocollector.DefaultDownloadRetention,
		UnclaimedRetention: videocollector.DefaultUnclaimedRetention,
		MaxConcurrent:      config.maxConcurrent,
		MaxQueued:          config.maxQueued,
		TaskTimeout:        config.taskTimeout,
	}, engine)
	if err != nil {
		log.Fatalf("initialize video collector: %v", err)
	}
	defer manager.Close()

	handler, err := webapp.NewServer(webapp.ServerConfig{
		Manager:             manager,
		WebRoot:             config.webRoot,
		ParseRateLimit:      config.parseRateLimit,
		TaskRateLimit:       config.taskRateLimit,
		ParseRateWindow:     15 * time.Minute,
		TaskRateWindow:      time.Hour,
		TrustProxy:          config.trustProxy,
		EgressStatus:        func() string { return string(egress.Status()) },
		EmbedMode:           config.embedMode,
		EmbedAllowedOrigins: config.embedAllowedOrigins,
		EmbedSessionTTL:     config.embedSessionTTL,
		Runtime: webapp.RuntimeStatus{
			YTDLPVersion:   commandVersion(config.ytDLPPath, "--version"),
			FFmpegVersion:  commandVersion(config.ffmpegPath, "-version"),
			WhisperVersion: config.whisperVersion,
			WhisperModel:   filepath.Base(config.whisperModelPath),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	server := &http.Server{
		Addr:              config.listenAddress,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	cleanupStop := make(chan struct{})
	cleanupDone := make(chan struct{})
	go func() {
		defer close(cleanupDone)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				manager.CleanupExpired()
			case <-cleanupStop:
				return
			}
		}
	}()

	go func() {
		log.Printf("video collector listening on %s", config.listenAddress)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("video collector server failed: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	close(cleanupStop)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
	<-cleanupDone
}

type appConfig struct {
	listenAddress       string
	tempRoot            string
	webRoot             string
	ytDLPPath           string
	ffmpegPath          string
	whisperPath         string
	whisperModelPath    string
	whisperVersion      string
	maxConcurrent       int
	maxQueued           int
	parseRateLimit      int
	taskRateLimit       int
	taskTimeout         time.Duration
	trustProxy          bool
	embedMode           string
	embedAllowedOrigins []string
	embedSessionTTL     time.Duration
	egress              videocollector.EgressConfig
}

func loadConfig() (appConfig, error) {
	maxConcurrent := envInt("VIDEO_COLLECTOR_MAX_CONCURRENT", 2)
	if maxConcurrent < 1 || maxConcurrent > 8 {
		return appConfig{}, errors.New("VIDEO_COLLECTOR_MAX_CONCURRENT must be between 1 and 8")
	}
	maxQueued := envInt("VIDEO_COLLECTOR_MAX_QUEUED", 4)
	if maxQueued < 1 || maxQueued > 32 {
		return appConfig{}, errors.New("VIDEO_COLLECTOR_MAX_QUEUED must be between 1 and 32")
	}
	parseRateLimit := envInt("VIDEO_COLLECTOR_PARSE_RATE_LIMIT", 20)
	if parseRateLimit < 1 || parseRateLimit > 1000 {
		return appConfig{}, errors.New("VIDEO_COLLECTOR_PARSE_RATE_LIMIT must be between 1 and 1000")
	}
	taskRateLimit := envInt("VIDEO_COLLECTOR_TASK_RATE_LIMIT", 5)
	if taskRateLimit < 1 || taskRateLimit > 100 {
		return appConfig{}, errors.New("VIDEO_COLLECTOR_TASK_RATE_LIMIT must be between 1 and 100")
	}
	taskTimeoutSeconds := envInt("VIDEO_COLLECTOR_TASK_TIMEOUT_SECONDS", 7200)
	if taskTimeoutSeconds < 60 || taskTimeoutSeconds > 7200 {
		return appConfig{}, errors.New("VIDEO_COLLECTOR_TASK_TIMEOUT_SECONDS must be between 60 and 7200")
	}
	egress, err := loadEgressConfig()
	if err != nil {
		return appConfig{}, err
	}
	embedMode, embedAllowedOrigins, embedSessionTTL, err := loadEmbedConfig()
	if err != nil {
		return appConfig{}, err
	}
	config := appConfig{
		listenAddress:       envOrDefault("VIDEO_COLLECTOR_LISTEN", "127.0.0.1:8787"),
		tempRoot:            envOrDefault("VIDEO_COLLECTOR_TEMP_ROOT", "/app/cache/tasks"),
		webRoot:             envOrDefault("VIDEO_COLLECTOR_WEB_ROOT", "/app/web"),
		ytDLPPath:           envOrDefault("YTDLP_PATH", "/usr/local/bin/yt-dlp"),
		ffmpegPath:          envOrDefault("FFMPEG_PATH", "/usr/bin/ffmpeg"),
		whisperPath:         envOrDefault("WHISPER_PATH", "/usr/local/bin/whisper-cli"),
		whisperModelPath:    envOrDefault("WHISPER_MODEL_PATH", "/app/models/ggml-base.bin"),
		whisperVersion:      envOrDefault("WHISPER_VERSION", "whisper.cpp"),
		maxConcurrent:       maxConcurrent,
		maxQueued:           maxQueued,
		parseRateLimit:      parseRateLimit,
		taskRateLimit:       taskRateLimit,
		taskTimeout:         time.Duration(taskTimeoutSeconds) * time.Second,
		trustProxy:          envBool("VIDEO_COLLECTOR_TRUST_PROXY", false),
		embedMode:           embedMode,
		embedAllowedOrigins: embedAllowedOrigins,
		embedSessionTTL:     embedSessionTTL,
		egress:              egress,
	}
	return config, nil
}

func loadEmbedConfig() (string, []string, time.Duration, error) {
	mode := strings.ToLower(envOrDefault("VIDEO_COLLECTOR_EMBED_MODE", webapp.EmbedModeOff))
	if mode != webapp.EmbedModeOff && mode != webapp.EmbedModeSoft {
		return "", nil, 0, errors.New("VIDEO_COLLECTOR_EMBED_MODE must be off or soft")
	}
	ttlSeconds, err := envIntBetween("VIDEO_COLLECTOR_EMBED_SESSION_TTL_SECONDS", 3600, 60, 86400)
	if err != nil {
		return "", nil, 0, err
	}
	rawOrigins := strings.TrimSpace(os.Getenv("VIDEO_COLLECTOR_EMBED_ALLOWED_ORIGINS"))
	if mode == webapp.EmbedModeOff && rawOrigins == "" {
		return mode, nil, time.Duration(ttlSeconds) * time.Second, nil
	}
	if rawOrigins == "" {
		return "", nil, 0, errors.New("VIDEO_COLLECTOR_EMBED_ALLOWED_ORIGINS is required in soft mode")
	}
	origins := make([]string, 0, 2)
	for _, value := range strings.Split(rawOrigins, ",") {
		origin := strings.TrimSpace(value)
		if origin == "" {
			return "", nil, 0, errors.New("VIDEO_COLLECTOR_EMBED_ALLOWED_ORIGINS contains an empty rule")
		}
		origins = append(origins, origin)
	}
	return mode, origins, time.Duration(ttlSeconds) * time.Second, nil
}

func loadEgressConfig() (videocollector.EgressConfig, error) {
	mode := videocollector.EgressMode(strings.ToLower(envOrDefault("VIDEO_COLLECTOR_EGRESS_MODE", string(videocollector.EgressModeOff))))
	if mode != videocollector.EgressModeOff && mode != videocollector.EgressModeAuto {
		return videocollector.EgressConfig{}, errors.New("VIDEO_COLLECTOR_EGRESS_MODE must be off or auto")
	}
	routeTTLSeconds, err := envIntBetween("VIDEO_COLLECTOR_CN_PROXY_ROUTE_TTL_SECONDS", 1800, 60, 3600)
	if err != nil {
		return videocollector.EgressConfig{}, err
	}
	connectTimeoutSeconds, err := envIntBetween("VIDEO_COLLECTOR_CN_PROXY_CONNECT_TIMEOUT_SECONDS", 5, 1, 15)
	if err != nil {
		return videocollector.EgressConfig{}, err
	}
	breakerFailures, err := envIntBetween("VIDEO_COLLECTOR_CN_PROXY_BREAKER_FAILURES", 3, 1, 10)
	if err != nil {
		return videocollector.EgressConfig{}, err
	}
	breakerSeconds, err := envIntBetween("VIDEO_COLLECTOR_CN_PROXY_BREAKER_SECONDS", 60, 10, 600)
	if err != nil {
		return videocollector.EgressConfig{}, err
	}
	config := videocollector.EgressConfig{
		Mode:            mode,
		RouteTTL:        time.Duration(routeTTLSeconds) * time.Second,
		ConnectTimeout:  time.Duration(connectTimeoutSeconds) * time.Second,
		BreakerFailures: breakerFailures,
		BreakerDuration: time.Duration(breakerSeconds) * time.Second,
	}
	if mode == videocollector.EgressModeOff {
		return config, nil
	}
	config.ProxyURL = strings.TrimSpace(os.Getenv("VIDEO_COLLECTOR_CN_PROXY_URL"))
	rawHosts := os.Getenv("VIDEO_COLLECTOR_CN_PROXY_SOURCE_HOSTS")
	if strings.TrimSpace(rawHosts) == "" {
		return videocollector.EgressConfig{}, errors.New("VIDEO_COLLECTOR_CN_PROXY_SOURCE_HOSTS is required in auto mode")
	}
	for _, value := range strings.Split(rawHosts, ",") {
		host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
		if host == "" {
			return videocollector.EgressConfig{}, errors.New("VIDEO_COLLECTOR_CN_PROXY_SOURCE_HOSTS contains an empty rule")
		}
		config.SourceHosts = append(config.SourceHosts, host)
	}
	if _, err := videocollector.NewEgressRouter(config); err != nil {
		return videocollector.EgressConfig{}, fmt.Errorf("invalid domestic egress configuration: %w", err)
	}
	return config, nil
}

func commandVersion(path string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, path, args...).CombinedOutput()
	if err != nil {
		return "unavailable"
	}
	line := strings.SplitN(strings.TrimSpace(string(output)), "\n", 2)[0]
	return strings.TrimSpace(line)
}

func envInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return -1
	}
	return value
}

func envIntBetween(name string, fallback, minimum, maximum int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || value > maximum {
		return 0, fmt.Errorf("%s must be between %d and %d", name, minimum, maximum)
	}
	return value, nil
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envBool(name string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}

func init() {
	log.SetFlags(log.Ldate | log.Ltime | log.LUTC)
	log.SetPrefix("video-collector: ")
}
