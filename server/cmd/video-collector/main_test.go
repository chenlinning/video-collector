package main

import (
	"testing"
	"time"

	"github.com/chenlinning/video-collector/server/internal/videocollector"
	"github.com/chenlinning/video-collector/server/internal/webapp"
	"github.com/stretchr/testify/require"
)

func TestLoadConfigAllowsSixtyMinuteTranscriptionsToFinish(t *testing.T) {
	clearEgressEnvironment(t)
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
	require.Equal(t, videocollector.EgressModeOff, config.egress.Mode)
}

func TestLoadConfigValidatesAutomaticEgress(t *testing.T) {
	clearEgressEnvironment(t)
	t.Setenv("VIDEO_COLLECTOR_EGRESS_MODE", "auto")
	_, err := loadConfig()
	require.Error(t, err)

	t.Setenv("VIDEO_COLLECTOR_CN_PROXY_URL", "http://10.77.0.2:3128")
	t.Setenv("VIDEO_COLLECTOR_CN_PROXY_SOURCE_HOSTS", " BILIBILI.COM. , b23.tv ")
	config, err := loadConfig()
	require.NoError(t, err)
	require.Equal(t, videocollector.EgressModeAuto, config.egress.Mode)
	require.Equal(t, []string{"bilibili.com", "b23.tv"}, config.egress.SourceHosts)
	require.Equal(t, 30*time.Minute, config.egress.RouteTTL)
	require.Equal(t, 5*time.Second, config.egress.ConnectTimeout)
}

func TestLoadEmbedConfigDefaultsToDisabled(t *testing.T) {
	clearEgressEnvironment(t)
	clearEmbedEnvironment(t)
	mode, origins, ttl, err := loadEmbedConfig()
	require.NoError(t, err)
	require.Equal(t, webapp.EmbedModeOff, mode)
	require.Empty(t, origins)
	require.Equal(t, time.Hour, ttl)
}

func TestLoadEmbedConfigValidatesSoftMode(t *testing.T) {
	clearEgressEnvironment(t)
	clearEmbedEnvironment(t)
	t.Setenv("VIDEO_COLLECTOR_EMBED_MODE", "soft")
	_, _, _, err := loadEmbedConfig()
	require.Error(t, err)

	t.Setenv("VIDEO_COLLECTOR_EMBED_ALLOWED_ORIGINS", "https://ximoai.cn, https://www.ximoai.cn")
	t.Setenv("VIDEO_COLLECTOR_EMBED_SESSION_TTL_SECONDS", "7200")
	mode, origins, ttl, err := loadEmbedConfig()
	require.NoError(t, err)
	require.Equal(t, webapp.EmbedModeSoft, mode)
	require.Equal(t, []string{"https://ximoai.cn", "https://www.ximoai.cn"}, origins)
	require.Equal(t, 2*time.Hour, ttl)

	t.Setenv("VIDEO_COLLECTOR_EMBED_ALLOWED_ORIGINS", "https://ximoai.cn,,https://www.ximoai.cn")
	_, _, _, err = loadEmbedConfig()
	require.Error(t, err)
}

func TestLoadConfigRejectsUnsafeEgressValues(t *testing.T) {
	tests := []struct {
		name     string
		proxyURL string
		hosts    string
		variable string
		value    string
	}{
		{name: "https proxy", proxyURL: "https://10.77.0.2:3128", hosts: "bilibili.com"},
		{name: "public proxy", proxyURL: "http://203.0.113.8:3128", hosts: "bilibili.com"},
		{name: "proxy hostname", proxyURL: "http://proxy.example.com:3128", hosts: "bilibili.com"},
		{name: "proxy credentials", proxyURL: "http://user:pass@10.77.0.2:3128", hosts: "bilibili.com"},
		{name: "proxy path", proxyURL: "http://10.77.0.2:3128/path", hosts: "bilibili.com"},
		{name: "empty host rule", proxyURL: "http://10.77.0.2:3128", hosts: "bilibili.com,,b23.tv"},
		{name: "host with scheme", proxyURL: "http://10.77.0.2:3128", hosts: "https://bilibili.com"},
		{name: "top level host rule", proxyURL: "http://10.77.0.2:3128", hosts: "com"},
		{name: "ttl too small", proxyURL: "http://10.77.0.2:3128", hosts: "bilibili.com", variable: "VIDEO_COLLECTOR_CN_PROXY_ROUTE_TTL_SECONDS", value: "59"},
		{name: "timeout too large", proxyURL: "http://10.77.0.2:3128", hosts: "bilibili.com", variable: "VIDEO_COLLECTOR_CN_PROXY_CONNECT_TIMEOUT_SECONDS", value: "16"},
		{name: "breaker failures invalid", proxyURL: "http://10.77.0.2:3128", hosts: "bilibili.com", variable: "VIDEO_COLLECTOR_CN_PROXY_BREAKER_FAILURES", value: "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEgressEnvironment(t)
			t.Setenv("VIDEO_COLLECTOR_EGRESS_MODE", "auto")
			t.Setenv("VIDEO_COLLECTOR_CN_PROXY_URL", tt.proxyURL)
			t.Setenv("VIDEO_COLLECTOR_CN_PROXY_SOURCE_HOSTS", tt.hosts)
			if tt.variable != "" {
				t.Setenv(tt.variable, tt.value)
			}
			_, err := loadConfig()
			require.Error(t, err)
		})
	}
}

func clearEgressEnvironment(t *testing.T) {
	for _, name := range []string{
		"VIDEO_COLLECTOR_EGRESS_MODE", "VIDEO_COLLECTOR_CN_PROXY_URL",
		"VIDEO_COLLECTOR_CN_PROXY_SOURCE_HOSTS", "VIDEO_COLLECTOR_CN_PROXY_ROUTE_TTL_SECONDS",
		"VIDEO_COLLECTOR_CN_PROXY_CONNECT_TIMEOUT_SECONDS", "VIDEO_COLLECTOR_CN_PROXY_BREAKER_FAILURES",
		"VIDEO_COLLECTOR_CN_PROXY_BREAKER_SECONDS",
	} {
		t.Setenv(name, "")
	}
}

func clearEmbedEnvironment(t *testing.T) {
	for _, name := range []string{
		"VIDEO_COLLECTOR_EMBED_MODE", "VIDEO_COLLECTOR_EMBED_ALLOWED_ORIGINS",
		"VIDEO_COLLECTOR_EMBED_SESSION_TTL_SECONDS",
	} {
		t.Setenv(name, "")
	}
}
