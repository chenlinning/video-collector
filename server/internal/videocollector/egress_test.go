package videocollector

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEgressRouterDefaultsToDirectAndOnlyMatchesConfiguredHosts(t *testing.T) {
	router, err := NewEgressRouter(EgressConfig{Mode: EgressModeOff})
	require.NoError(t, err)
	require.Equal(t, EgressDirect, router.Decide("www.bilibili.com").Route)
	_, ok := router.Fallback("www.bilibili.com", EgressDecision{Route: EgressDirect}, EgressFailureRetryable)
	require.False(t, ok)
	require.Equal(t, EgressStatusOff, router.Status())

	router, err = NewEgressRouter(EgressConfig{
		Mode: EgressModeAuto, ProxyURL: "http://10.77.0.2:3128",
		SourceHosts: []string{"BILIBILI.COM."}, RouteTTL: time.Minute,
		ConnectTimeout: 5 * time.Second, BreakerFailures: 3, BreakerDuration: time.Minute,
	})
	require.NoError(t, err)
	require.Equal(t, EgressDirect, router.Decide("example.com").Route)
	_, ok = router.Fallback("example.com", EgressDecision{Route: EgressDirect}, EgressFailureRetryable)
	require.False(t, ok)

	initial := router.Decide("WWW.BILIBILI.COM.")
	require.Equal(t, EgressDirect, initial.Route)
	fallback, ok := router.Fallback("www.bilibili.com", initial, EgressFailureRetryable)
	require.True(t, ok)
	require.Equal(t, EgressCNProxy, fallback.Route)
	require.Equal(t, "http://10.77.0.2:3128", fallback.proxyURL)
}

func TestEgressRouterCachesSuccessfulProxyRouteUntilTTL(t *testing.T) {
	now := time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC)
	router, err := NewEgressRouter(EgressConfig{
		Mode: EgressModeAuto, ProxyURL: "http://10.77.0.2:3128", SourceHosts: []string{"bilibili.com"},
		RouteTTL: time.Minute, ConnectTimeout: time.Second, BreakerFailures: 3,
		BreakerDuration: time.Minute, Now: func() time.Time { return now },
	})
	require.NoError(t, err)

	direct := router.Decide("www.bilibili.com")
	proxy, ok := router.Fallback("www.bilibili.com", direct, EgressFailureRetryable)
	require.True(t, ok)
	router.Report("www.bilibili.com", proxy, EgressFailureNone)
	require.Equal(t, EgressCNProxy, router.Decide("www.bilibili.com").Route)

	now = now.Add(time.Minute + time.Second)
	require.Equal(t, EgressDirect, router.Decide("www.bilibili.com").Route)
}

func TestEgressRouterDoesNotFallbackForPermanentFailures(t *testing.T) {
	router, err := NewEgressRouter(EgressConfig{
		Mode: EgressModeAuto, ProxyURL: "http://10.77.0.2:3128", SourceHosts: []string{"bilibili.com"},
		RouteTTL: time.Minute, ConnectTimeout: time.Second, BreakerFailures: 3, BreakerDuration: time.Minute,
	})
	require.NoError(t, err)
	_, ok := router.Fallback("bilibili.com", router.Decide("bilibili.com"), EgressFailurePermanent)
	require.False(t, ok)

	require.Equal(t, EgressFailurePermanent, classifyEgressFailure("ERROR: HTTP Error 404: Not Found", errors.New("exit status 1")))
	require.Equal(t, EgressFailurePermanent, classifyEgressFailure("Sign in to confirm you are not a bot; cookies are required", errors.New("exit status 1")))
	require.Equal(t, EgressFailureRetryable, classifyEgressFailure("ERROR: HTTP Error 412: Precondition Failed", errors.New("exit status 1")))
	require.Equal(t, EgressFailureProxyUnavailable, classifyEgressFailure("ProxyError: unable to connect to proxy", errors.New("connection refused")))
}

func TestEgressRouterCircuitBreakerAllowsOneHalfOpenProbe(t *testing.T) {
	now := time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC)
	router, err := NewEgressRouter(EgressConfig{
		Mode: EgressModeAuto, ProxyURL: "http://10.77.0.2:3128", SourceHosts: []string{"bilibili.com"},
		RouteTTL: time.Minute, ConnectTimeout: time.Second, BreakerFailures: 2,
		BreakerDuration: time.Minute, Now: func() time.Time { return now },
	})
	require.NoError(t, err)

	for range 2 {
		direct := router.Decide("bilibili.com")
		proxy, ok := router.Fallback("bilibili.com", direct, EgressFailureRetryable)
		require.True(t, ok)
		router.Report("bilibili.com", proxy, EgressFailureProxyUnavailable)
	}
	require.Equal(t, EgressStatusUnavailable, router.Status())
	_, ok := router.Fallback("bilibili.com", router.Decide("bilibili.com"), EgressFailureRetryable)
	require.False(t, ok)

	now = now.Add(time.Minute + time.Second)
	probe, ok := router.Fallback("bilibili.com", router.Decide("bilibili.com"), EgressFailureRetryable)
	require.True(t, ok)
	_, secondProbe := router.Fallback("bilibili.com", router.Decide("bilibili.com"), EgressFailureRetryable)
	require.False(t, secondProbe)
	router.Report("bilibili.com", probe, EgressFailureNone)
	require.Equal(t, EgressStatusAvailable, router.Status())
}

func TestEgressRouterReleasesHalfOpenProbeWhenSwitchCleanupFails(t *testing.T) {
	now := time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC)
	router, err := NewEgressRouter(EgressConfig{
		Mode: EgressModeAuto, ProxyURL: "http://10.77.0.2:3128", SourceHosts: []string{"bilibili.com"},
		RouteTTL: time.Minute, ConnectTimeout: time.Second, BreakerFailures: 1,
		BreakerDuration: time.Minute, Now: func() time.Time { return now },
	})
	require.NoError(t, err)
	proxy, ok := router.Fallback("bilibili.com", router.Decide("bilibili.com"), EgressFailureRetryable)
	require.True(t, ok)
	router.Report("bilibili.com", proxy, EgressFailureProxyUnavailable)
	now = now.Add(time.Minute + time.Second)

	_, _, err = executeWithEgress(context.Background(), router, "bilibili.com", func() error {
		return errors.New("cleanup failed")
	}, func(EgressDecision) (string, string, error) {
		return "", "HTTP Error 412", errors.New("exit status 1")
	})
	require.EqualError(t, err, "cleanup failed")
	_, ok = router.Fallback("bilibili.com", router.Decide("bilibili.com"), EgressFailureRetryable)
	require.True(t, ok)
}

func TestEgressRouterCancellationDoesNotCloseAnUnverifiedHalfOpenProbe(t *testing.T) {
	now := time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC)
	router, err := NewEgressRouter(EgressConfig{
		Mode: EgressModeAuto, ProxyURL: "http://10.77.0.2:3128", SourceHosts: []string{"bilibili.com"},
		RouteTTL: time.Minute, ConnectTimeout: time.Second, BreakerFailures: 1,
		BreakerDuration: time.Minute, Now: func() time.Time { return now },
	})
	require.NoError(t, err)
	proxy, ok := router.Fallback("bilibili.com", router.Decide("bilibili.com"), EgressFailureRetryable)
	require.True(t, ok)
	router.Report("bilibili.com", proxy, EgressFailureProxyUnavailable)
	now = now.Add(time.Minute + time.Second)
	probe, ok := router.Fallback("bilibili.com", router.Decide("bilibili.com"), EgressFailureRetryable)
	require.True(t, ok)
	router.Report("bilibili.com", probe, EgressFailureCancelled)
	require.Equal(t, EgressStatusDegraded, router.Status())
	_, ok = router.Fallback("bilibili.com", router.Decide("bilibili.com"), EgressFailureRetryable)
	require.True(t, ok)
}

func TestEgressRouterStateIsConcurrentSafe(t *testing.T) {
	router, err := NewEgressRouter(EgressConfig{
		Mode: EgressModeAuto, ProxyURL: "http://10.77.0.2:3128", SourceHosts: []string{"bilibili.com"},
		RouteTTL: time.Minute, ConnectTimeout: time.Second, BreakerFailures: 3, BreakerDuration: time.Minute,
	})
	require.NoError(t, err)

	var workers sync.WaitGroup
	for range 32 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 50 {
				decision := router.Decide("www.bilibili.com")
				if decision.Route == EgressDirect {
					if proxy, ok := router.Fallback("www.bilibili.com", decision, EgressFailureRetryable); ok {
						router.Report("www.bilibili.com", proxy, EgressFailureNone)
					}
				} else {
					router.Report("www.bilibili.com", decision, EgressFailureNone)
				}
			}
		}()
	}
	workers.Wait()
	require.NotEqual(t, EgressStatusUnavailable, router.Status())
}

func TestExecuteWithEgressSwitchesAtMostOnce(t *testing.T) {
	router, err := NewEgressRouter(EgressConfig{
		Mode: EgressModeAuto, ProxyURL: "http://10.77.0.2:3128", SourceHosts: []string{"bilibili.com"},
		RouteTTL: time.Minute, ConnectTimeout: time.Second, BreakerFailures: 3, BreakerDuration: time.Minute,
	})
	require.NoError(t, err)
	var routes []EgressRoute
	value, _, err := executeWithEgress(context.Background(), router, "www.bilibili.com", nil, func(decision EgressDecision) (string, string, error) {
		routes = append(routes, decision.Route)
		if decision.Route == EgressDirect {
			return "", "HTTP Error 412", errors.New("exit status 1")
		}
		return "ok", "", nil
	})
	require.NoError(t, err)
	require.Equal(t, "ok", value)
	require.Equal(t, []EgressRoute{EgressDirect, EgressCNProxy}, routes)
}

func TestExecuteWithEgressFallsBackForTimeout(t *testing.T) {
	router, err := NewEgressRouter(EgressConfig{
		Mode: EgressModeAuto, ProxyURL: "http://10.77.0.2:3128", SourceHosts: []string{"bilibili.com"},
		RouteTTL: time.Minute, ConnectTimeout: time.Second, BreakerFailures: 3, BreakerDuration: time.Minute,
	})
	require.NoError(t, err)
	var routes []EgressRoute
	_, _, err = executeWithEgress(context.Background(), router, "bilibili.com", nil, func(decision EgressDecision) (string, string, error) {
		routes = append(routes, decision.Route)
		if decision.Route == EgressDirect {
			return "", "connection timed out", errors.New("exit status 1")
		}
		return "ok", "", nil
	})
	require.NoError(t, err)
	require.Equal(t, []EgressRoute{EgressDirect, EgressCNProxy}, routes)
}

func TestExecuteWithEgressDoesNotProxyPermanentOrUncontrolledFailures(t *testing.T) {
	router, err := NewEgressRouter(EgressConfig{
		Mode: EgressModeAuto, ProxyURL: "http://10.77.0.2:3128", SourceHosts: []string{"bilibili.com"},
		RouteTTL: time.Minute, ConnectTimeout: time.Second, BreakerFailures: 3, BreakerDuration: time.Minute,
	})
	require.NoError(t, err)

	for _, test := range []struct {
		name   string
		host   string
		stderr string
	}{
		{name: "permanent", host: "bilibili.com", stderr: "HTTP Error 404: Not Found"},
		{name: "uncontrolled", host: "example.com", stderr: "HTTP Error 412: Precondition Failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			attempts := 0
			_, _, err := executeWithEgress(context.Background(), router, test.host, nil, func(decision EgressDecision) (string, string, error) {
				attempts++
				require.Equal(t, EgressDirect, decision.Route)
				return "", test.stderr, errors.New("exit status 1")
			})
			require.Error(t, err)
			require.Equal(t, 1, attempts)
		})
	}
}
