// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/pki"
)

func setCA(ca *pki.CA) func(*model.State) error {
	return func(s *model.State) error {
		s.CACert, s.CAKey = string(ca.CertPEM()), string(ca.KeyPEM())
		return nil
	}
}

func newCA(t *testing.T) *pki.CA {
	t.Helper()
	ca, err := pki.NewCA(t0)
	must(t, err)
	return ca
}

// The CA is what every node trusts, and the file is its only copy.
func TestTheCASurvivesARestartAndStaysPrivate(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	ca := newCA(t)
	must(t, s.Update(setCA(ca)))

	if fi, err := os.Stat(s.Path()); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("state.json: %v, %v; the CA key is in it, so it has to be 0600", fi, err)
	}
	if raw := string(readFile(t, s.Path())); !strings.Contains(raw, "BEGIN EC PRIVATE KEY") || !strings.Contains(raw, "BEGIN CERTIFICATE") {
		t.Fatal("state.json doesn't hold the CA")
	}

	again := open(t, dir)
	got := again.Read()
	loaded, err := pki.Load([]byte(got.CACert), []byte(got.CAKey))
	if err != nil || !loaded.Certificate().Equal(ca.Certificate()) {
		t.Fatalf("after a restart the CA is %v, %v", loaded, err)
	}
	if again.Writes() != 0 {
		t.Fatalf("reopening wrote the file %d times", again.Writes())
	}
}

func TestAnUpdateCannotLeaveABrokenCA(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	ca := newCA(t)
	must(t, s.Update(setCA(ca)))
	before, writes := s.Read(), s.Writes()
	file := readFile(t, s.Path())

	for name, edit := range map[string]func(*model.State) error{
		"the key dropped":    func(st *model.State) error { st.CAKey = ""; return nil },
		"another CA's key":   func(st *model.State) error { st.CAKey = string(newCA(t).KeyPEM()); return nil },
		"text for the pair":  func(st *model.State) error { st.CACert, st.CAKey = "cert", "key"; return nil },
		"the pair taken out": func(st *model.State) error { st.CACert, st.CAKey = "", ""; return nil },
	} {
		if err := s.Update(edit); err == nil {
			t.Fatalf("%s: the update went through", name)
		}
		if !same(t, s.Read(), before) || s.Writes() != writes || string(readFile(t, s.Path())) != string(file) {
			t.Fatalf("%s: a refused update moved the state or the file", name)
		}
	}
}

func TestAFileWithABrokenCAIsCorruptAndKept(t *testing.T) {
	good := model.NewState()
	ca, other := newCA(t), newCA(t)
	good.CACert, good.CAKey = string(ca.CertPEM()), string(other.KeyPEM()) // not a pair
	content, err := model.Encode(good)
	must(t, err)

	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	must(t, os.WriteFile(path, content, 0o600))
	_, err = Open(dir)
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
	if string(readFile(t, path)) != string(content) {
		t.Fatal("the file with the CA was changed")
	}
	if copies, _ := filepath.Glob(path + ".corrupt-*"); len(copies) != 1 {
		t.Fatalf("copies = %v, want one", copies)
	}
	// the error names the problem and doesn't print the key it found
	if secret := string(other.KeyPEM()); strings.Contains(err.Error(), strings.SplitN(secret, "\n", 3)[1][:24]) {
		t.Fatalf("the error quotes the key: %v", err)
	}
}
