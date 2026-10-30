// SPDX-License-Identifier: AGPL-3.0-or-later

package bridgectl

import (
	"bytes"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"
)

func TestRingKeepsEverythingWhileItFits(t *testing.T) {
	r := newRing(16)
	if r.String() != "" {
		t.Fatalf("new ring holds %q", r.String())
	}
	r.Write([]byte("hello\n"))
	r.Write([]byte("world\n"))
	if got := r.String(); got != "hello\nworld\n" {
		t.Fatalf("got %q", got)
	}
}

func TestRingKeepsTheTail(t *testing.T) {
	r := newRing(16)
	for i := 0; i < 10; i++ {
		fmt.Fprintf(r, "line %d\n", i) // 7 bytes each
	}
	// 70 bytes written, 16 kept: "e 8\nline 9\n" and what precedes, less the cut line.
	got := string(r.bytes())
	if want := "ine 8\nline 9\n"; !strings.HasSuffix(got, want) || len(got) != 16 {
		t.Fatalf("holds %q", got)
	}
	if s := r.String(); s != "line 8\nline 9\n" {
		t.Fatalf("String() = %q: the cut first line should be gone", s)
	}
}

func TestRingFillsExactly(t *testing.T) {
	r := newRing(8)
	r.Write([]byte("12345678"))
	if got := r.String(); got != "12345678" {
		t.Fatalf("got %q", got)
	}
	r.Write([]byte("9"))
	if got := string(r.bytes()); got != "23456789" {
		t.Fatalf("got %q", got)
	}
}

func TestRingWriteLargerThanItself(t *testing.T) {
	r := newRing(4)
	r.Write([]byte("ab"))
	if n, err := r.Write([]byte("0123456789")); n != 10 || err != nil {
		t.Fatalf("Write returned %d, %v", n, err)
	}
	if got := string(r.bytes()); got != "6789" {
		t.Fatalf("got %q", got)
	}
	r.Write([]byte("x"))
	if got := string(r.bytes()); got != "789x" {
		t.Fatalf("got %q", got)
	}
}

func TestRingEmptyWrite(t *testing.T) {
	r := newRing(4)
	if n, err := r.Write(nil); n != 0 || err != nil {
		t.Fatalf("Write returned %d, %v", n, err)
	}
	if r.String() != "" {
		t.Fatal("an empty write put something in")
	}
}

// Against a model that keeps everything and takes the tail, for any pattern
// of writes the ring holds exactly the last size bytes and never more.
func TestRingMatchesAModel(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for _, size := range []int{1, 2, 7, 64, 1000} {
		r := newRing(size)
		var all []byte
		for i := 0; i < 500; i++ {
			chunk := make([]byte, rng.Intn(3*size+2))
			for j := range chunk {
				chunk[j] = byte('a' + rng.Intn(26))
			}
			if n, err := r.Write(chunk); n != len(chunk) || err != nil {
				t.Fatalf("Write: %d, %v", n, err)
			}
			all = append(all, chunk...)
			want := all
			if len(want) > size {
				want = want[len(want)-size:]
			}
			if got := r.bytes(); !bytes.Equal(got, want) {
				t.Fatalf("size %d after %d writes: holds %q, want %q", size, i+1, got, want)
			}
		}
	}
}

func TestRingConcurrentWrites(t *testing.T) {
	r := newRing(256)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				fmt.Fprintf(r, "writer %d line %d\n", i, j)
				_ = r.String()
			}
		}()
	}
	wg.Wait()
	if n := len(r.bytes()); n != 256 {
		t.Fatalf("holds %d bytes", n)
	}
}
