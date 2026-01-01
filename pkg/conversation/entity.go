package conversation

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// SessionEntity represents the database table for user conversations
type SessionEntity struct {
	UserID    int64     `gorm:"primaryKey;autoIncrement:false"`
	State     string    `gorm:"size:100;index"`
	Data      JSONMap   `gorm:"type:text"` // Stores the wizard context
	UpdatedAt time.Time `gorm:"index"`     // Used for cleanup
}

func (SessionEntity) TableName() string {
	return "conversation_sessions"
}

// JSONMap is a helper for GORM to handle map[string]interface{} <-> JSONB
type JSONMap map[string]interface{}

func (j JSONMap) Value() (driver.Value, error) {
	return json.Marshal(j)
}

func (j *JSONMap) Scan(value interface{}) error {
	if value == nil {
		*j = make(map[string]interface{})
		return nil
	}
	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, j)
	case string:
		return json.Unmarshal([]byte(v), j)
	default:
		return fmt.Errorf("JSONMap.Scan: unsupported type %T", value)
	}
}
