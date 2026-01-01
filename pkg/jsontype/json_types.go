// Package jsontype provides reusable typed wrappers for JSONB database columns.
package jsontype

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// StringSlice: []string ↔ PostgreSQL JSONB.
// GORM: `gorm:"serializer:json;type:jsonb"`.
type StringSlice []string

// Value implements driver.Valuer interface for database writes
func (s StringSlice) Value() (driver.Value, error) {
	if s == nil {
		return nil, nil
	}
	return json.Marshal(s)
}

// Scan implements sql.Scanner interface for database reads
func (s *StringSlice) Scan(value interface{}) error {
	if value == nil {
		*s = nil
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("StringSlice.Scan: expected []byte or string, got %T", value)
	}

	return json.Unmarshal(bytes, s)
}

// Contains checks if the slice contains the given value
func (s StringSlice) Contains(value string) bool {
	for _, v := range s {
		if v == value {
			return true
		}
	}
	return false
}

// StringMap is a typed wrapper for map[string]string stored as JSONB.
// Useful for storing key-value pairs like headers or metadata.
type StringMap map[string]string

// Value implements driver.Valuer interface for database writes
func (m StringMap) Value() (driver.Value, error) {
	if m == nil {
		return nil, nil
	}
	return json.Marshal(m)
}

// Scan implements sql.Scanner interface for database reads
func (m *StringMap) Scan(value interface{}) error {
	if value == nil {
		*m = nil
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("StringMap.Scan: expected []byte or string, got %T", value)
	}

	return json.Unmarshal(bytes, m)
}
