package domain

import (
	"fmt"
	"strconv"
)

// ValueType preserves legacy numeric text values while allowing explicit strings.
func (c CustomSockopt) ValueType() string {
	if c.Type != "" {
		return c.Type
	}
	if _, err := strconv.ParseInt(fmt.Sprint(c.OptValue), 10, 64); err == nil {
		return "int"
	}
	return "str"
}

func (s *SockoptSettings) ValidateCustomOptions() error {
	if s == nil {
		return nil
	}
	for i, option := range s.CustomSockopt {
		switch option.ValueType() {
		case "int":
			if _, err := strconv.ParseInt(fmt.Sprint(option.OptValue), 10, 32); err != nil {
				return fmt.Errorf("custom socket option %d: int value must be a signed 32-bit integer", i+1)
			}
		case "str":
			if _, ok := option.OptValue.(string); !ok {
				return fmt.Errorf("custom socket option %d: str value must be a string", i+1)
			}
		default:
			return fmt.Errorf("custom socket option %d: type must be int or str", i+1)
		}
	}
	return nil
}
