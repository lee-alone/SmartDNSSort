package main

import (
	"fmt"
	"net/url"
	"strings"
)

func main() {
	tests := []string{
		"tls://1.1.1.1",
		"tls://dns.google",
		"https://dns.cloudflare.com/dns-query",
		"https://1.1.1.1/dns-query",
		"udp://8.8.8.8:53",
		"8.8.8.8:53", // 无 scheme
	}

	for _, t := range tests {
		fmt.Printf("Testing: %s\n", t)
		if !strings.Contains(t, "://") {
			fmt.Println("  -> No scheme, treated as UDP/TCP address directly")
			continue
		}
		u, err := url.Parse(t)
		if err != nil {
			fmt.Printf("  -> Error: %v\n", err)
			continue
		}
		fmt.Printf("  -> Scheme: %s, Host: %s, Path: %s\n", u.Scheme, u.Host, u.Path)
	}
}
