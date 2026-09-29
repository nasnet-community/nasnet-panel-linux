package starlink

import (
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DishError is a non-OK status the dish itself returned, relayed through the
// node agent. Kept apart from agent/transport errors so callers can tell
// "dish refused" from "agent too old".
type DishError struct {
	Code    codes.Code
	Message string
}

func (e *DishError) Error() string {
	return fmt.Sprintf("dish: %s: %s", e.Code, e.Message)
}

// DishCode returns the dish status code carried by err: a relayed DishError,
// or a gRPC status from a direct dish channel.
func DishCode(err error) codes.Code {
	var de *DishError
	if errors.As(err, &de) {
		return de.Code
	}
	return status.Code(err)
}
