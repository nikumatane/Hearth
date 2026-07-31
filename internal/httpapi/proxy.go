package httpapi

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

const (
	maxForwardedHeaderBytes = 1024
	maxForwardedHops        = 8
)

var defaultTrustedProxyCIDRs = []string{"127.0.0.0/8", "::1/128"}

type proxyResolver struct {
	trusted []netip.Prefix
}

func newProxyResolver(cidrs []string) (*proxyResolver, error) {
	if cidrs == nil {
		cidrs = defaultTrustedProxyCIDRs
	}
	resolver := &proxyResolver{trusted: make([]netip.Prefix, 0, len(cidrs))}
	for _, cidr := range cidrs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(cidr))
		if err != nil {
			return nil, fmt.Errorf("invalid trusted proxy CIDR %q: %w", cidr, err)
		}
		resolver.trusted = append(resolver.trusted, prefix.Masked())
	}
	return resolver, nil
}

func (p *proxyResolver) clientIP(r *http.Request) string {
	remote, ok := parseRemoteAddress(r.RemoteAddr)
	if !ok {
		return truncateSource(strings.TrimSpace(r.RemoteAddr))
	}
	if !p.isTrusted(remote) {
		return remote.String()
	}
	values, ok := forwardedValues(r.Header.Values("X-Forwarded-For"))
	if !ok || len(values) == 0 {
		return remote.String()
	}
	for index := len(values) - 1; index >= 0; index-- {
		candidate, err := netip.ParseAddr(values[index])
		if err != nil {
			return remote.String()
		}
		candidate = candidate.Unmap()
		if !p.isTrusted(candidate) {
			return candidate.String()
		}
	}
	return remote.String()
}

func (p *proxyResolver) forwardedProto(r *http.Request) string {
	remote, ok := parseRemoteAddress(r.RemoteAddr)
	if !ok || !p.isTrusted(remote) {
		return ""
	}
	values, ok := forwardedValues(r.Header.Values("X-Forwarded-Proto"))
	if !ok || len(values) == 0 {
		return ""
	}
	for _, value := range values {
		switch strings.ToLower(value) {
		case "https", "http":
		default:
			return ""
		}
	}
	return strings.ToLower(values[len(values)-1])
}

func (p *proxyResolver) isTrusted(address netip.Addr) bool {
	address = address.Unmap()
	for _, prefix := range p.trusted {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func (p *proxyResolver) ruleAddressAllowed(address netip.Addr) bool {
	address = address.Unmap()
	return address.IsValid() && !address.IsUnspecified() && !address.IsMulticast() &&
		!address.IsLoopback() && !p.isTrusted(address)
}

func parseRemoteAddress(value string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(strings.TrimSpace(value))
	if err != nil {
		host = strings.Trim(strings.TrimSpace(value), "[]")
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return address.Unmap(), true
}

func forwardedValues(headers []string) ([]string, bool) {
	totalBytes := 0
	values := make([]string, 0, len(headers))
	for _, header := range headers {
		totalBytes += len(header)
		if totalBytes > maxForwardedHeaderBytes {
			return nil, false
		}
		for _, value := range strings.Split(header, ",") {
			value = strings.Trim(strings.TrimSpace(value), "[]")
			if value == "" {
				return nil, false
			}
			values = append(values, value)
			if len(values) > maxForwardedHops {
				return nil, false
			}
		}
	}
	return values, true
}

func truncateSource(value string) string {
	const maxSourceLength = 128
	if len(value) <= maxSourceLength {
		return value
	}
	return value[:maxSourceLength]
}
