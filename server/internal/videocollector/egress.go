package videocollector

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type EgressMode string

const (
	EgressModeOff  EgressMode = "off"
	EgressModeAuto EgressMode = "auto"
)

type EgressRoute string

const (
	EgressDirect  EgressRoute = "direct"
	EgressCNProxy EgressRoute = "cn_proxy"
)

type EgressStatus string

const (
	EgressStatusOff         EgressStatus = "off"
	EgressStatusAvailable   EgressStatus = "available"
	EgressStatusDegraded    EgressStatus = "degraded"
	EgressStatusUnavailable EgressStatus = "unavailable"
)

type EgressFailureClass string

const (
	EgressFailureNone             EgressFailureClass = "none"
	EgressFailureRetryable        EgressFailureClass = "retryable"
	EgressFailurePermanent        EgressFailureClass = "permanent"
	EgressFailureProxyUnavailable EgressFailureClass = "proxy_unavailable"
	EgressFailureCancelled        EgressFailureClass = "cancelled"
)

type EgressConfig struct {
	Mode            EgressMode
	ProxyURL        string
	SourceHosts     []string
	RouteTTL        time.Duration
	ConnectTimeout  time.Duration
	BreakerFailures int
	BreakerDuration time.Duration
	Now             func() time.Time
}

type EgressDecision struct {
	Route          EgressRoute
	proxyURL       string
	connectTimeout time.Duration
	halfOpenProbe  bool
}

type EgressRouter struct {
	mu               sync.Mutex
	mode             EgressMode
	proxyURL         string
	sourceHosts      []string
	routeTTL         time.Duration
	connectTimeout   time.Duration
	breakerFailures  int
	breakerDuration  time.Duration
	now              func() time.Time
	proxyUntil       map[string]time.Time
	failureCount     int
	breakerOpenUntil time.Time
	halfOpenInFlight bool
}

func NewEgressRouter(config EgressConfig) (*EgressRouter, error) {
	if config.Mode == "" {
		config.Mode = EgressModeOff
	}
	if config.Mode != EgressModeOff && config.Mode != EgressModeAuto {
		return nil, errors.New("egress mode must be off or auto")
	}
	if config.RouteTTL <= 0 {
		config.RouteTTL = 30 * time.Minute
	}
	if config.ConnectTimeout <= 0 {
		config.ConnectTimeout = 5 * time.Second
	}
	if config.BreakerFailures <= 0 {
		config.BreakerFailures = 3
	}
	if config.BreakerDuration <= 0 {
		config.BreakerDuration = time.Minute
	}
	if config.Now == nil {
		config.Now = time.Now
	}

	router := &EgressRouter{
		mode: config.Mode, routeTTL: config.RouteTTL, connectTimeout: config.ConnectTimeout,
		breakerFailures: config.BreakerFailures, breakerDuration: config.BreakerDuration,
		now: config.Now, proxyUntil: make(map[string]time.Time),
	}
	if config.Mode == EgressModeOff {
		return router, nil
	}

	proxyURL, err := validateEgressProxyURL(config.ProxyURL)
	if err != nil {
		return nil, err
	}
	if len(config.SourceHosts) == 0 {
		return nil, errors.New("automatic egress requires source host rules")
	}
	hosts := make([]string, 0, len(config.SourceHosts))
	seen := make(map[string]struct{}, len(config.SourceHosts))
	for _, value := range config.SourceHosts {
		host, err := normalizeSourceHostRule(value)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[host]; exists {
			continue
		}
		seen[host] = struct{}{}
		hosts = append(hosts, host)
	}
	router.proxyURL = proxyURL
	router.sourceHosts = hosts
	return router, nil
}

func (r *EgressRouter) Decide(host string) EgressDecision {
	if r == nil {
		return EgressDecision{Route: EgressDirect}
	}
	host = normalizeEgressHost(host)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.mode != EgressModeAuto || !r.matchesHost(host) {
		return EgressDecision{Route: EgressDirect}
	}
	now := r.now()
	if until, ok := r.proxyUntil[host]; ok {
		if now.Before(until) {
			if decision, allowed := r.proxyDecisionLocked(now); allowed {
				return decision
			}
		} else {
			delete(r.proxyUntil, host)
		}
	}
	return EgressDecision{Route: EgressDirect}
}

func (r *EgressRouter) Fallback(host string, current EgressDecision, failure EgressFailureClass) (EgressDecision, bool) {
	if r == nil || (failure != EgressFailureRetryable && failure != EgressFailureProxyUnavailable) {
		return EgressDecision{}, false
	}
	host = normalizeEgressHost(host)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.mode != EgressModeAuto || !r.matchesHost(host) {
		return EgressDecision{}, false
	}
	if current.Route == EgressCNProxy {
		return EgressDecision{Route: EgressDirect}, true
	}
	if current.Route != EgressDirect {
		return EgressDecision{}, false
	}
	return r.proxyDecisionLocked(r.now())
}

func (r *EgressRouter) Report(host string, decision EgressDecision, failure EgressFailureClass) {
	if r == nil {
		return
	}
	host = normalizeEgressHost(host)
	r.mu.Lock()
	defer r.mu.Unlock()
	if decision.Route == EgressDirect {
		if failure == EgressFailureNone {
			delete(r.proxyUntil, host)
		}
		return
	}
	if decision.Route != EgressCNProxy {
		return
	}
	if decision.halfOpenProbe {
		r.halfOpenInFlight = false
	}
	if failure == EgressFailureCancelled {
		return
	}
	delete(r.proxyUntil, host)
	if failure == EgressFailureNone {
		r.failureCount = 0
		r.breakerOpenUntil = time.Time{}
		r.proxyUntil[host] = r.now().Add(r.routeTTL)
		return
	}
	if failure != EgressFailureProxyUnavailable {
		// A target response proves that the private proxy itself is reachable.
		r.failureCount = 0
		r.breakerOpenUntil = time.Time{}
		return
	}
	r.failureCount++
	if decision.halfOpenProbe || r.failureCount >= r.breakerFailures {
		r.breakerOpenUntil = r.now().Add(r.breakerDuration)
	}
}

func (r *EgressRouter) Abort(decision EgressDecision) {
	if r == nil || !decision.halfOpenProbe {
		return
	}
	r.mu.Lock()
	r.halfOpenInFlight = false
	r.mu.Unlock()
}

func (r *EgressRouter) Status() EgressStatus {
	if r == nil {
		return EgressStatusOff
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.mode == EgressModeOff {
		return EgressStatusOff
	}
	now := r.now()
	if !r.breakerOpenUntil.IsZero() && now.Before(r.breakerOpenUntil) {
		return EgressStatusUnavailable
	}
	if !r.breakerOpenUntil.IsZero() || r.failureCount > 0 || r.halfOpenInFlight {
		return EgressStatusDegraded
	}
	return EgressStatusAvailable
}

func (r *EgressRouter) proxyDecisionLocked(now time.Time) (EgressDecision, bool) {
	if !r.breakerOpenUntil.IsZero() {
		if now.Before(r.breakerOpenUntil) || r.halfOpenInFlight {
			return EgressDecision{}, false
		}
		r.halfOpenInFlight = true
		return EgressDecision{
			Route: EgressCNProxy, proxyURL: r.proxyURL, connectTimeout: r.connectTimeout, halfOpenProbe: true,
		}, true
	}
	return EgressDecision{Route: EgressCNProxy, proxyURL: r.proxyURL, connectTimeout: r.connectTimeout}, true
}

func (r *EgressRouter) matchesHost(host string) bool {
	if host == "" {
		return false
	}
	for _, rule := range r.sourceHosts {
		if host == rule || strings.HasSuffix(host, "."+rule) {
			return true
		}
	}
	return false
}

func (d EgressDecision) ytDLPArgs() []string {
	if d.Route != EgressCNProxy || d.proxyURL == "" {
		return nil
	}
	args := []string{"--proxy", d.proxyURL}
	if d.connectTimeout > 0 {
		seconds := max(1, int(d.connectTimeout/time.Second))
		args = append(args, "--socket-timeout", strconv.Itoa(seconds))
	}
	return args
}

func executeWithEgress[T any](
	ctx context.Context,
	router *EgressRouter,
	host string,
	cleanup func() error,
	run func(EgressDecision) (T, string, error),
) (T, string, error) {
	decision := EgressDecision{Route: EgressDirect}
	if router != nil {
		decision = router.Decide(host)
	}
	switched := false
	for {
		value, stderr, err := run(decision)
		failure := classifyEgressFailure(stderr, err)
		if err == nil {
			if router != nil {
				router.Report(host, decision, EgressFailureNone)
			}
			return value, stderr, nil
		}
		if router != nil {
			router.Report(host, decision, failure)
		}
		if switched || router == nil {
			return value, stderr, err
		}
		next, ok := router.Fallback(host, decision, failure)
		if !ok {
			return value, stderr, err
		}
		if cleanup != nil {
			if cleanupErr := cleanup(); cleanupErr != nil {
				router.Abort(next)
				var zero T
				return zero, "", cleanupErr
			}
		}
		decision = next
		switched = true
		select {
		case <-ctx.Done():
			var zero T
			return zero, "", ctx.Err()
		default:
		}
	}
}

func classifyEgressFailure(stderr string, err error) EgressFailureClass {
	if err == nil {
		return EgressFailureNone
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return EgressFailureCancelled
	}
	message := strings.ToLower(stderr + "\n" + err.Error())
	for _, marker := range []string{
		"unable to connect to proxy", "proxy connection failed", "proxyerror", "proxy error",
		"tunnel connection failed", "connect to proxy", "proxyconnect tcp",
	} {
		if strings.Contains(message, marker) {
			return EgressFailureProxyUnavailable
		}
	}
	for _, marker := range []string{
		"http error 400", "http error 401", "http error 404", "login required", "sign in",
		"log in", "cookies are required", "cookie is required", "captcha", "drm", "paid content",
		"members-only", "private video", "unsupported url", "video has been removed", "content has been removed",
	} {
		if strings.Contains(message, marker) {
			return EgressFailurePermanent
		}
	}
	for _, marker := range []string{
		"http error 403", "http error 408", "http error 412", "http error 429", "http error 451",
		"connection reset", "remote end closed connection", "remote server closed connection",
		"connection refused",
		"temporary failure in name resolution", "name resolution failed", "timed out", "timeout",
		"geo-restricted", "geo restricted", "not available in your country", "not available in your region",
		"region restricted", "country restricted", "ip address blocked", "blocked your ip",
	} {
		if strings.Contains(message, marker) {
			return EgressFailureRetryable
		}
	}
	return EgressFailurePermanent
}

func validateEgressProxyURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil ||
		parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("CN proxy URL must be an HTTP URL without credentials, path, query, or fragment")
	}
	proxyIP := net.ParseIP(parsed.Hostname())
	if proxyIP == nil || proxyIP.To4() == nil || !proxyIP.IsPrivate() || proxyIP.IsLoopback() || proxyIP.IsLinkLocalUnicast() {
		return "", errors.New("CN proxy URL must use a fixed private IPv4 WireGuard address")
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 {
		return "", errors.New("CN proxy URL must include a valid port")
	}
	return parsed.String(), nil
}

func normalizeSourceHostRule(value string) (string, error) {
	host := normalizeEgressHost(value)
	if host == "" || len(host) > 253 || !strings.Contains(host, ".") || net.ParseIP(host) != nil || strings.ContainsAny(host, "/:*@?#") {
		return "", errors.New("CN proxy source host rule is invalid")
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("CN proxy source host rule is invalid")
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return "", errors.New("CN proxy source host rule is invalid")
			}
		}
	}
	return host, nil
}

func normalizeEgressHost(value string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
}
