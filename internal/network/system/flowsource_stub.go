//go:build !linux

package system

import (
	"context"
	"errors"
)

func (s *LiveFlowSource) Conntrack(context.Context) ([]CTFlow, error) {
	return nil, errors.New("conntrack requires Linux")
}
