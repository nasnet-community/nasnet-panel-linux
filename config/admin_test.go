package config

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestAdminConfigValidateBcrypt(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("installation-test"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	valid := string(hash)
	for _, revision := range []string{"2", "2a", "2b", "2x", "2y"} {
		t.Run(revision, func(t *testing.T) {
			candidate := "$" + revision + valid[3:]
			if err := (AdminConfig{Username: "admin", PasswordHash: candidate}).Validate(); err != nil {
				t.Fatal(err)
			}
			if err := bcrypt.CompareHashAndPassword([]byte(candidate), []byte("installation-test")); err != nil {
				t.Fatalf("accepted hash must remain usable for login: %v", err)
			}
		})
	}
	for name, candidate := range map[string]string{
		"missing":           "",
		"compose truncated": "$2a$10",
		"missing checksum":  valid[:29],
		"short checksum":    valid[:len(valid)-1],
		"long checksum":     valid + "a",
		"bad character":     valid[:len(valid)-1] + "!",
		"bad cost":          valid[:4] + "99" + valid[6:],
		"not bcrypt":        "password",
	} {
		t.Run(name, func(t *testing.T) {
			err := (AdminConfig{Username: "admin", PasswordHash: candidate}).Validate()
			if err == nil || !strings.Contains(err.Error(), "ADMIN_PASSWORD_HASH") || !strings.Contains(err.Error(), "single quotes") {
				t.Fatalf("expected actionable hash error, got %v", err)
			}
			if candidate != "" && strings.Contains(err.Error(), candidate) {
				t.Fatal("error exposed the configured hash")
			}
		})
	}
	for _, username := range []string{"", " \t"} {
		if err := (AdminConfig{Username: username, PasswordHash: valid}).Validate(); err == nil || !strings.Contains(err.Error(), "ADMIN_USERNAME") {
			t.Fatalf("expected username error, got %v", err)
		}
	}
}
