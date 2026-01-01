#!/usr/bin/env bash
# scripts/pre-commit-secrets.sh
# Pre-commit hook: block commits that contain likely secrets.
# Install: ln -sf ../../scripts/pre-commit-secrets.sh .git/hooks/pre-commit

set -euo pipefail

PATTERNS=(
  'PRIVATE.KEY'
  'BEGIN RSA PRIVATE KEY'
  'BEGIN EC PRIVATE KEY'
  'BEGIN OPENSSH PRIVATE KEY'
  'password\s*[:=]\s*["\x27][^"\x27]{8,}'
  'secret\s*[:=]\s*["\x27][^"\x27]{8,}'
  'token\s*[:=]\s*["\x27][^"\x27]{8,}'
  'BOT_TOKEN\s*=\s*[0-9]+:[A-Za-z0-9_-]+'
  'JWT_SECRET'
)

BLOCKED_FILES=('.env' '*.pem' '*.key' 'id_rsa' 'id_ed25519')

EXIT_CODE=0

# Check staged file names
for pattern in "${BLOCKED_FILES[@]}"; do
  MATCHES=$(git diff --cached --name-only --diff-filter=ACR | grep -E "(^|/)${pattern}$" || true)
  if [ -n "$MATCHES" ]; then
    echo "ERROR: Blocked file staged for commit:"
    echo "$MATCHES"
    EXIT_CODE=1
  fi
done

# Check staged file contents for secret patterns
for pattern in "${PATTERNS[@]}"; do
  MATCHES=$(git diff --cached -U0 --diff-filter=ACM | grep -iE "^\+.*${pattern}" || true)
  if [ -n "$MATCHES" ]; then
    echo "ERROR: Possible secret detected in staged changes:"
    echo "  Pattern: $pattern"
    echo "$MATCHES" | head -3
    EXIT_CODE=1
  fi
done

if [ $EXIT_CODE -ne 0 ]; then
  echo ""
  echo "Commit blocked. If this is a false positive, use: git commit --no-verify"
  exit 1
fi
