package signing

import "crypto/ed25519"

// PublicKey: Ed25519 pubkey for verifying agent binary signatures.
// Populate via `go run ./cmd/sign-agent --generate-key` and paste the
// resulting `var PublicKey = ed25519.PublicKey{...}` line here.
var PublicKey ed25519.PublicKey
