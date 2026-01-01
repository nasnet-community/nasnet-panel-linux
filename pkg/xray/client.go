package xray

import (
	"github.com/google/uuid"
)

// GenerateUUID creates a new UUID for xray client
func GenerateUUID() string {
	return uuid.New().String()
}
