package restream

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net"
	"net/url"
	"strings"
)

func NormalizeSource(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("stream URL is required")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", errors.New("stream URL is invalid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("stream URL must use http or https")
	}
	if parsed.Hostname() == "" {
		return "", errors.New("stream URL must include a host")
	}
	if isBlockedHost(parsed.Hostname()) {
		return "", errors.New("local and private network URLs are not allowed")
	}
	if !isAllowedYouTubeHost(parsed.Hostname()) {
		return "", errors.New("only youtube.com and youtu.be URLs are allowed")
	}

	parsed.Fragment = ""
	return parsed.String(), nil
}

func StableSessionID(source string) string {
	sum := sha256.Sum256([]byte(source))
	return base64.RawURLEncoding.EncodeToString(sum[:16])
}

func isBlockedHost(host string) bool {
	name := strings.TrimSuffix(strings.ToLower(host), ".")
	if name == "localhost" {
		return true
	}

	ip := net.ParseIP(name)
	if ip == nil {
		return false
	}

	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

func isAllowedYouTubeHost(host string) bool {
	name := strings.TrimSuffix(strings.ToLower(host), ".")
	return name == "youtu.be" || name == "youtube.com" || name == "www.youtube.com" || strings.HasSuffix(name, ".youtube.com")
}
