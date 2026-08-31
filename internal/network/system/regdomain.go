package system

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

var regCodeRe = regexp.MustCompile(`^[A-Z]{2}$`)

// RegDomainIsUnset reports the useless default. "00" is the world domain: it
// marks nearly all 5 GHz NO_IR, and an AP started under it dies with
// "Channel N (primary) not allowed for AP mode", which reads like a driver bug.
func RegDomainIsUnset(code string) bool {
	c := normalizeRegDomain(code)
	return c == "" || c == "00" || c == "0"
}

func ReadRegDomain(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "iw", "reg", "get").Output()
	if err != nil {
		return "", fmt.Errorf("iw reg get: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if _, rest, ok := strings.Cut(strings.TrimSpace(line), "country "); ok {
			code, _, _ := strings.Cut(rest, ":")
			return normalizeRegDomain(code), nil
		}
	}
	return "", fmt.Errorf("no country line in `iw reg get` output")
}

// SetRegDomain must run BEFORE hostapd. Changing it afterwards does not
// re-evaluate a channel hostapd already refused.
func SetRegDomain(ctx context.Context, code string) error {
	c := normalizeRegDomain(code)
	if !regCodeRe.MatchString(c) {
		return fmt.Errorf("country code %q must be two letters (ISO 3166-1 alpha-2)", code)
	}
	if ctx == nil {
		// Format-check only, for callers validating before an apply
		return nil
	}
	out, err := exec.CommandContext(ctx, "iw", "reg", "set", c).CombinedOutput()
	if err != nil {
		return fmt.Errorf("iw reg set %s: %w (output: %s)", c, err, strings.TrimSpace(string(out)))
	}
	return nil
}
