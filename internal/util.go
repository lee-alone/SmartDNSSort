package util

import (
	"net"
	"strings"
)

// IsIPv4 检查是否为 IPv4 地址
func IsIPv4(ip string) bool {
	return net.ParseIP(ip).To4() != nil
}

// IsIPv6 检查是否为 IPv6 地址
func IsIPv6(ip string) bool {
	return net.ParseIP(ip) != nil && net.ParseIP(ip).To4() == nil
}

// FilterIPv4 从 IP 列表中筛选 IPv4
func FilterIPv4(ips []string) []string {
	var result []string
	for _, ip := range ips {
		if IsIPv4(ip) {
			result = append(result, ip)
		}
	}
	return result
}

// FilterIPv6 从 IP 列表中筛选 IPv6
func FilterIPv6(ips []string) []string {
	var result []string
	for _, ip := range ips {
		if IsIPv6(ip) {
			result = append(result, ip)
		}
	}
	return result
}

// NormalizeDomain 规范化域名
func NormalizeDomain(domain string) string {
	return strings.TrimRight(strings.ToLower(domain), ".")
}

// IsValidDomain 验证域名格式
func IsValidDomain(domain string) bool {
	domain = strings.TrimRight(domain, ".")
	if len(domain) == 0 || len(domain) > 255 {
		return false
	}

	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return false
	}

	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 {
			return false
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}

		for _, ch := range label {
			if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
				(ch >= '0' && ch <= '9') || ch == '-') {
				return false
			}
		}
	}

	return true
}
