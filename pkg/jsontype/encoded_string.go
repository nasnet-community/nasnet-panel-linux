package jsontype

import (
	"context"
	"encoding/json"
	"fmt"
	"gorm.io/gorm/schema"
	"reflect"
)

type encodedStringSerializer struct{}

func init() { schema.RegisterSerializer("encoded_string", encodedStringSerializer{}) }

// Accept legacy plaintext while storing new values safely for PostgreSQL text.
func (encodedStringSerializer) Scan(ctx context.Context, field *schema.Field, dst reflect.Value, value interface{}) error {
	var text string
	switch v := value.(type) {
	case nil:
	case string:
		text = v
	case []byte:
		text = string(v)
	default:
		return fmt.Errorf("invalid encoded string value")
	}
	var decoded string
	if len(text) > 0 && text[0] == '"' && json.Unmarshal([]byte(text), &decoded) == nil {
		text = decoded
	}
	field.ReflectValueOf(ctx, dst).SetString(text)
	return nil
}
func (encodedStringSerializer) Value(_ context.Context, _ *schema.Field, _ reflect.Value, value interface{}) (interface{}, error) {
	text, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("encoded string expects a string")
	}
	data, err := json.Marshal(text)
	return string(data), err
}
