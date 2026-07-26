package videocollector

import "strings"

type ExtractorFailureKind string

const (
	ExtractorFailurePlatformRestricted     ExtractorFailureKind = "platform_restricted"
	ExtractorFailureAuthenticationRequired ExtractorFailureKind = "authentication_required"
	ExtractorFailureDRMProtected           ExtractorFailureKind = "drm_protected"
	ExtractorFailureUnsupportedURL         ExtractorFailureKind = "unsupported_url"
	ExtractorFailureMediaUnavailable       ExtractorFailureKind = "media_unavailable"
	ExtractorFailureTemporary              ExtractorFailureKind = "temporary_failure"
	ExtractorFailureUnknown                ExtractorFailureKind = "unknown"
)

type ExtractorFailure struct {
	Kind   ExtractorFailureKind
	Detail string
	Cause  error
}

func (failure *ExtractorFailure) Error() string {
	switch failure.Kind {
	case ExtractorFailurePlatformRestricted:
		return "平台拒绝了公开解析请求，请稍后重试或更换无需登录的公开链接"
	case ExtractorFailureAuthenticationRequired:
		return "该内容需要登录、Cookie 或会员权限，本项目不提供此类访问"
	case ExtractorFailureDRMProtected:
		return "该内容受 DRM 保护，本项目不提供绕过功能"
	case ExtractorFailureUnsupportedURL:
		return "当前解析器不支持此链接或页面类型"
	case ExtractorFailureMediaUnavailable:
		return "该内容已删除、私密或无法公开访问"
	case ExtractorFailureTemporary:
		return "平台暂时无法访问，请稍后重试"
	default:
		return "媒体解析失败，请确认链接可公开访问后重试"
	}
}

func (failure *ExtractorFailure) Unwrap() error {
	return failure.Cause
}

func classifyExtractorFailure(stderr string, err error) ExtractorFailureKind {
	message := strings.ToLower(stderr)
	if err != nil {
		message += "\n" + strings.ToLower(err.Error())
	}
	if containsAny(message, "drm", "encrypted media") {
		return ExtractorFailureDRMProtected
	}
	if (strings.Contains(message, "cookies") && strings.Contains(message, "needed")) ||
		containsAny(message,
			"http error 401", "login required", "sign in", "log in", "cookies are required",
			"cookie is required", "members-only", "premium members only", "paid content",
		) {
		return ExtractorFailureAuthenticationRequired
	}
	if containsAny(message, "unsupported url", "no suitable extractor") {
		return ExtractorFailureUnsupportedURL
	}
	if containsAny(message,
		"http error 403", "http error 412", "http error 451", "captcha",
		"geo-restricted", "geo restricted", "not available in your country", "not available in your region",
		"region restricted", "country restricted", "ip address blocked", "blocked your ip",
	) {
		return ExtractorFailurePlatformRestricted
	}
	if containsAny(message,
		"http error 404", "video has been removed", "content has been removed", "private video",
		"no longer available", "does not exist", "not found",
	) {
		return ExtractorFailureMediaUnavailable
	}
	if containsAny(message,
		"http error 408", "http error 429", "http error 500", "http error 502", "http error 503", "http error 504",
		"connection reset", "remote end closed connection", "remote server closed connection", "connection refused",
		"temporary failure in name resolution", "name resolution failed", "timed out", "timeout",
	) {
		return ExtractorFailureTemporary
	}
	return ExtractorFailureUnknown
}

func containsAny(value string, markers ...string) bool {
	for _, marker := range markers {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}
