// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
)

func parseCert(p []byte) (*x509.Certificate, error) {
	b, _ := pem.Decode(p)
	if b == nil {
		return nil, errors.New("not PEM")
	}
	return x509.ParseCertificate(b.Bytes)
}
