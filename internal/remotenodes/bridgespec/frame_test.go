// SPDX-License-Identifier: AGPL-3.0-or-later

package bridgespec

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

func frameOf(payload []byte) []byte {
	out := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(out, uint32(len(payload)))
	copy(out[4:], payload)
	return out
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestFrameRoundTrip(t *testing.T) {
	want := validSpec()
	want.Nodes = append(want.Nodes, extraNode(2))
	want.LogLevel = "info"
	want.Exit.Blocked = []string{"10.0.0.0/8", "fc00::/7"}

	var buf bytes.Buffer
	if err := Write(&buf, want); err != nil {
		t.Fatal(err)
	}
	frame := buf.Bytes()
	if n := binary.BigEndian.Uint32(frame); int(n) != len(frame)-4 {
		t.Fatalf("length prefix %d, payload %d bytes", n, len(frame)-4)
	}
	got, err := Read(bytes.NewReader(frame))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip changed the spec:\n got %+v\nwant %+v", got, want)
	}
}

func TestEncodeMatchesWrite(t *testing.T) {
	frame, err := Encode(validSpec())
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := Write(&buf, validSpec()); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(frame, buf.Bytes()) {
		t.Fatal("Encode and Write differ")
	}
}

// The hub writes into a pipe, so the whole frame has to go in one call.
func TestWriteIsOneCall(t *testing.T) {
	var w countingWriter
	if err := Write(&w, validSpec()); err != nil {
		t.Fatal(err)
	}
	if w.calls != 1 {
		t.Fatalf("%d Write calls", w.calls)
	}
}

type countingWriter struct{ calls int }

func (w *countingWriter) Write(p []byte) (int, error) { w.calls++; return len(p), nil }

func TestWriteRefusesInvalidSpec(t *testing.T) {
	s := validSpec()
	s.Nodes[0].UUID = "nope"
	var buf bytes.Buffer
	if err := Write(&buf, s); err == nil {
		t.Fatal("wrote an invalid spec")
	}
	if buf.Len() != 0 {
		t.Fatalf("%d bytes written before the error", buf.Len())
	}
	if _, err := Encode(s); err == nil {
		t.Fatal("encoded an invalid spec")
	}
}

func TestWritePassesOnWriterError(t *testing.T) {
	boom := errors.New("pipe closed")
	if err := Write(errWriter{boom}, validSpec()); !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
}

type errWriter struct{ err error }

func (w errWriter) Write([]byte) (int, error) { return 0, w.err }

// Read takes one frame and nothing more: the bridge goes on watching what
// is left of its stdin for the hub hanging up.
func TestReadLeavesTheRest(t *testing.T) {
	frame, err := Encode(validSpec())
	if err != nil {
		t.Fatal(err)
	}
	r := bytes.NewReader(append(frame, "tail"...))
	if _, err := Read(r); err != nil {
		t.Fatal(err)
	}
	rest, _ := io.ReadAll(r)
	if string(rest) != "tail" {
		t.Fatalf("left %q", rest)
	}
}

func TestReadInSmallPieces(t *testing.T) {
	frame, err := Encode(validSpec())
	if err != nil {
		t.Fatal(err)
	}
	// One byte per Read, as a slow pipe may hand it over.
	if _, err := Read(iotest.OneByteReader(bytes.NewReader(frame))); err != nil {
		t.Fatal(err)
	}
}

func TestReadErrors(t *testing.T) {
	good := mustJSON(t, validSpec())
	tests := []struct {
		name  string
		input []byte
		is    error  // errors.Is target, if any
		want  string // part of the message, if any
	}{
		{"nothing at all", nil, io.EOF, "no spec"},
		{"half a length", []byte{0, 0}, io.ErrUnexpectedEOF, "length"},
		{"zero length", frameOf(nil), ErrEmpty, ""},
		{"length over the cap", binary.BigEndian.AppendUint32(nil, MaxSize+1), ErrTooLarge, ""},
		{"length of 4 GiB", []byte{0xff, 0xff, 0xff, 0xff}, ErrTooLarge, ""},
		{"payload cut short", frameOf(good)[:len(good)], io.ErrUnexpectedEOF, "bytes of spec"},
		{"payload missing", frameOf(good)[:4], io.EOF, "bytes of spec"},
		{"not JSON", frameOf([]byte("not json")), nil, "invalid character"},
		{"JSON of another shape", frameOf([]byte(`["a"]`)), nil, "cannot unmarshal"},
		{"null", frameOf([]byte("null")), nil, "version"},
		{"empty object", frameOf([]byte("{}")), nil, "version"},
		{"cut-off JSON", frameOf(good[:len(good)/2]), nil, "unexpected EOF"},
		{"JSON then more JSON", frameOf(append(append([]byte{}, good...), "{}"...)), nil, "after the spec"},
		{"JSON then junk", frameOf(append(append([]byte{}, good...), " x"...)), nil, "after the spec"},
		{"unknown field", frameOf(bytes.Replace(good, []byte(`"version"`), []byte(`"surprise":1,"version"`), 1)), nil, "unknown field"},
		{"wrong type", frameOf(bytes.Replace(good, []byte(`"port":443`), []byte(`"port":"443"`), 1)), nil, "port"},
		{"port out of range", frameOf(bytes.Replace(good, []byte(`"port":443`), []byte(`"port":70000`), 1)), nil, "port"},
		{"valid JSON, invalid spec", frameOf(bytes.Replace(good, []byte(`"dial_addr":"192.168.50.18"`), []byte(`"dial_addr":"router"`), 1)), nil, "dial_addr"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := Read(bytes.NewReader(tt.input))
			if err == nil {
				t.Fatalf("accepted: %v", s)
			}
			if tt.is != nil && !errors.Is(err, tt.is) {
				t.Errorf("error %q is not %v", err, tt.is)
			}
			if tt.want != "" && !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}

// A bad length must be refused on the prefix alone, without waiting for, or
// allocating, the payload it promises.
func TestReadRefusesOversizeBeforeReading(t *testing.T) {
	r := &tripwire{data: binary.BigEndian.AppendUint32(nil, MaxSize+1)}
	if _, err := Read(r); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("got %v", err)
	}
	if r.over {
		t.Fatal("read past the length prefix")
	}
}

type tripwire struct {
	data []byte
	over bool
}

func (r *tripwire) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		r.over = true
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func TestSizeBoundary(t *testing.T) {
	good := mustJSON(t, validSpec())
	pad := func(total int) []byte {
		return append(append([]byte{}, good...), bytes.Repeat([]byte{' '}, total-len(good))...)
	}
	if _, err := Read(bytes.NewReader(frameOf(pad(MaxSize)))); err != nil {
		t.Fatalf("a spec of exactly %d bytes: %v", MaxSize, err)
	}
	if _, err := Parse(pad(MaxSize + 1)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("one byte over: %v", err)
	}
}

func TestParseReadsHubJSON(t *testing.T) {
	// The shape the hub's tests and the docs can rely on.
	doc := `{
		"version": 1,
		"nodes": [{
			"tag": "node-1", "addr": "203.0.113.10", "port": 443,
			"uuid": "7f8b1a9e-3c2d-4b5a-9e6f-0a1b2c3d4e5f",
			"public_key": "BwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwc",
			"short_id": "0123456789abcdef", "server_name": "www.example.com",
			"flow": "xtls-rprx-vision", "reverse_tag": "rvs-from-node-1"
		}],
		"dial_addr": "192.168.50.18",
		"exit": {"addr": "192.168.50.17"},
		"dns": {"servers": ["1.1.1.1"]},
		"status_addr": "127.0.0.1:18080"
	}`
	s, err := Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if s.Nodes[0].Tag != "node-1" || s.Exit.Addr != "192.168.50.17" || len(s.Exit.Blocked) != 0 {
		t.Fatalf("parsed %+v", s)
	}
}

func FuzzRead(f *testing.F) {
	valid, err := Encode(validSpec())
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add([]byte{})
	f.Add([]byte{0, 0, 0, 0})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, '{'})
	f.Add(frameOf([]byte(`{"version":1}`)))
	f.Fuzz(func(t *testing.T, data []byte) {
		s, err := Read(bytes.NewReader(data))
		if err != nil {
			return
		}
		// Whatever Read accepts must be something Write would send back.
		if err := s.Validate(); err != nil {
			t.Fatalf("Read returned a spec that does not validate: %v", err)
		}
		if _, err := Encode(s); err != nil {
			t.Fatalf("Read returned a spec that does not encode: %v", err)
		}
	})
}

func TestStatusJSON(t *testing.T) {
	since := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	in := Status{
		Running: true,
		Started: since,
		Xray:    "26.10.21",
		Nodes: []NodeStatus{
			{Tag: "n1", Up: true, Since: since, BytesUp: 10, BytesDown: 20, Connections: 3},
			{Tag: "n2", Since: since},
		},
	}
	raw := mustJSON(t, in)
	// Field names are the contract with the hub's UI.
	for _, key := range []string{`"running":true`, `"started":"2026-10-30T12:00:00Z"`, `"xray":"26.10.21"`,
		`"tag":"n1"`, `"up":true`, `"since":`, `"bytes_up":10`, `"bytes_down":20`, `"connections":3`} {
		if !bytes.Contains(raw, []byte(key)) {
			t.Errorf("%s is missing from %s", key, raw)
		}
	}
	var out Status
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip: %+v != %+v", out, in)
	}
}
