package domain

import (
	"strconv"
	"strings"
)

// ListensOn reports whether the inbound may bind port: its Port, or any
// segment of its PortRange ("1000-2000" or "80,443").
func (ib *Inbound) ListensOn(port int) bool {
	if ib.Port == port {
		return true
	}
	if strings.TrimSpace(ib.PortRange) == "" {
		return false
	}
	for _, part := range strings.Split(ib.PortRange, ",") {
		lo, hi, isRange := strings.Cut(strings.TrimSpace(part), "-")
		if !isRange {
			hi = lo
		}
		l, err1 := strconv.Atoi(strings.TrimSpace(lo))
		h, err2 := strconv.Atoi(strings.TrimSpace(hi))
		if err1 == nil && err2 == nil && l <= port && port <= h {
			return true
		}
	}
	return false
}
