package config

import (
	"errors"
	"regexp"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// Cost alone accepts incomplete digests, so check the full encoded hash too.
// Include the original $2$ format and the commonly used revision markers.
var bcryptHashPattern = regexp.MustCompile(`^\$2[abxy]?\$[0-9]{2}\$[./A-Za-z0-9]{53}$`)

// Validate checks the effective credentials, after database overrides have been
// applied. A password changed in the panel takes precedence over the env hash.
func (c AdminConfig) Validate() error {
	if strings.TrimSpace(c.Username) == "" {
		return errors.New("ADMIN_USERNAME is required for panel login")
	}
	if !bcryptHashPattern.MatchString(c.PasswordHash) {
		return errors.New("ADMIN_PASSWORD_HASH must be a complete bcrypt hash; put the value in single quotes in .env (ADMIN_PASSWORD_HASH='...') so Docker Compose preserves its $ characters")
	}
	if _, err := bcrypt.Cost([]byte(c.PasswordHash)); err != nil {
		return errors.New("ADMIN_PASSWORD_HASH has an invalid bcrypt cost; generate a new bcrypt hash and put the value in single quotes in .env")
	}
	return nil
}
