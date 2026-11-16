// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build bridgetest

package main

import (
	"os"

	"github.com/xtls/xray-core/app/proxyman"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/transport/internet/tls"
)

// A test build trusts one more certificate authority for the outbounds' TLS:
// the one the end-to-end tests sign their servers with, as PEM in
// BRIDGE_TEST_ROOTS. Xray checks a server against its own copy of the public
// roots, which no setting of the system can add to. The shipped build is
// made without this file.
func init() {
	roots := os.Getenv("BRIDGE_TEST_ROOTS")
	if roots == "" {
		return
	}
	adjustConfig = func(c *core.Config) {
		for _, o := range c.Outbound {
			msg, err := o.SenderSettings.GetInstance()
			if err != nil {
				panic(err)
			}
			sender := msg.(*proxyman.SenderConfig)
			if sender.StreamSettings == nil {
				continue
			}
			for i, sec := range sender.StreamSettings.SecuritySettings {
				m, err := sec.GetInstance()
				if err != nil {
					panic(err)
				}
				if t, ok := m.(*tls.Config); ok {
					t.Certificate = append(t.Certificate, &tls.Certificate{Certificate: []byte(roots), Usage: tls.Certificate_AUTHORITY_VERIFY})
					sender.StreamSettings.SecuritySettings[i] = serial.ToTypedMessage(t)
				}
			}
			o.SenderSettings = serial.ToTypedMessage(sender)
		}
	}
}
