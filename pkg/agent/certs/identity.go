package certs

import (
	"crypto/x509"
	"fmt"
	"regexp"
	"strconv"
)

var nodeCNPattern = regexp.MustCompile(`^node-(\d+)-`)

// NodeIDFromCommonName returns the node ID embedded in an agent certificate
// CN ("node-<id>-<name>"). The hub's own certificates use id 0.
func NodeIDFromCommonName(cn string) (uint, bool) {
	m := nodeCNPattern.FindStringSubmatch(cn)
	if m == nil {
		return 0, false
	}
	id, err := strconv.ParseUint(m[1], 10, 32)
	if err != nil {
		return 0, false
	}
	return uint(id), true
}

// RejectNodeClient refuses a client certificate that belongs to a node: node
// certs are signed by the same CA as the hub's, but must not drive an agent.
func RejectNodeClient(leaf *x509.Certificate) error {
	if id, ok := NodeIDFromCommonName(leaf.Subject.CommonName); ok && id != 0 {
		return fmt.Errorf("node certificate %q cannot be used as a hub client", leaf.Subject.CommonName)
	}
	return nil
}
