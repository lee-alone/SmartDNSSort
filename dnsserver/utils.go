package dnsserver

import (
	"strconv"
	"strings"

	"github.com/miekg/dns"
)

// parseRcodeFromError attempts to extract the DNS Rcode from an error message.
// It looks for the pattern "rcode=N".
// If not found, it returns dns.RcodeServerFailure.
func parseRcodeFromError(err error) int {
	if err == nil {
		return dns.RcodeSuccess
	}

	errStr := err.Error()
	if strings.Contains(errStr, "rcode=") {
		parts := strings.Split(errStr, "rcode=")
		if len(parts) > 1 {
			// Try to parse the number immediately following "rcode="
			// We might need to trim non-numeric characters if the error message continues
			// But for now, Atoi might fail if there's trailing text, so let's be careful.
			// The original code just did Atoi(parts[1]), assuming parts[1] is just the number or starts with it?
			// strconv.Atoi("3") -> 3
			// strconv.Atoi("3 some other text") -> error
			// The original code:
			// rcodeInt, convErr := strconv.Atoi(parts[1])
			// So it assumes parts[1] is EXACTLY the number.
			// Let's stick to the original logic for now to be safe, or improve it.
			// Ideally we should extract the first sequence of digits.

			// Let's just replicate the original logic exactly to avoid regression,
			// but maybe clean up whitespace.
			token := strings.TrimSpace(parts[1])
			// If there are other words, we might need to split by space
			tokenParts := strings.Fields(token)
			if len(tokenParts) > 0 {
				token = tokenParts[0]
			}

			rcodeInt, convErr := strconv.Atoi(token)
			if convErr == nil {
				return rcodeInt
			}
		}
	}
	return dns.RcodeServerFailure
}
