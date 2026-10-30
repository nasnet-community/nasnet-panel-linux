// SPDX-License-Identifier: AGPL-3.0-or-later

package bridgespec

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// MaxSize caps a framed spec's JSON. A real one is a couple of kilobytes; the
// cap only stops a bad length prefix from asking for gigabytes.
const MaxSize = 64 << 10

var (
	// ErrEmpty is a frame that announces no payload.
	ErrEmpty = errors.New("bridgespec: empty frame")
	// ErrTooLarge is a frame, or a spec, over MaxSize.
	ErrTooLarge = errors.New("bridgespec: spec too large")
)

// Encode validates s and returns it as one frame: a 4-byte big-endian length,
// then the JSON.
func Encode(s *Spec) ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("bridgespec: %w", err)
	}
	payload, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("bridgespec: %w", err)
	}
	if len(payload) > MaxSize {
		return nil, fmt.Errorf("%w: %d bytes, limit %d", ErrTooLarge, len(payload), MaxSize)
	}
	frame := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(frame, uint32(len(payload)))
	copy(frame[4:], payload)
	return frame, nil
}

// Write sends s as one frame, in a single Write call.
func Write(w io.Writer, s *Spec) error {
	frame, err := Encode(s)
	if err != nil {
		return err
	}
	_, err = w.Write(frame)
	return err
}

// Read takes one frame from r and returns the validated spec. It reads
// exactly the frame, so the rest of r stays unread: the bridge keeps watching
// its stdin for the hub closing it.
func Read(r io.Reader) (*Spec, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("bridgespec: no spec before the input closed: %w", io.EOF)
		}
		return nil, fmt.Errorf("bridgespec: reading the length: %w", err)
	}
	n := binary.BigEndian.Uint32(hdr[:])
	switch {
	case n == 0:
		return nil, ErrEmpty
	case n > MaxSize:
		return nil, fmt.Errorf("%w: frame announces %d bytes, limit %d", ErrTooLarge, n, MaxSize)
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("bridgespec: reading %d bytes of spec: %w", n, err)
	}
	return Parse(payload)
}

// Parse reads a spec from its JSON: unknown fields and trailing data are
// errors, and the result is validated.
func Parse(data []byte) (*Spec, error) {
	if len(data) > MaxSize {
		return nil, fmt.Errorf("%w: %d bytes, limit %d", ErrTooLarge, len(data), MaxSize)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var s Spec
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("bridgespec: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("bridgespec: data after the spec")
	}
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("bridgespec: %w", err)
	}
	return &s, nil
}
