// SPDX-License-Identifier: AGPL-3.0-or-later

package backup

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	mrand "math/rand"
	"os"
	"strings"
	"testing"
	"time"
)

// TestMain keeps every test off parameters scrypt would choke on. The caps are
// what stop a crafted file from asking for gigabytes, so if one ever stopped
// holding, the real scrypt would be handed the very request it is meant to
// refuse, and the tests would run for minutes and eat the machine's memory
// instead of failing.
func TestMain(m *testing.M) {
	real := scryptKey
	scryptKey = func(pw, salt []byte, n, r, p, l int) ([]byte, error) {
		if n > maxN || r > maxR || p > maxP {
			return nil, fmt.Errorf("the test guard stopped scrypt from running with N %d, r %d, p %d", n, r, p)
		}
		return real(pw, salt, n, r, p, l)
	}
	os.Exit(m.Run())
}

const pass = "correct horse battery staple"

// fast is for the tests that make many files. The real cost is a tenth of a
// second, and half a second under the race detector.
var fast = kdf{n: 1 << 6, r: 8, p: 1}

var sample = []byte(`{"version":1,"nodes":[{"name":"تهران ۱","reality_private_key":"priv-secret"}]}` + "\n")

func sealFast(t *testing.T, snapshot []byte) []byte {
	t.Helper()
	f, err := seal(snapshot, pass, fast, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// forbidScrypt turns a call to scrypt into a failure, for the tests whose point
// is that a request is turned away before any work is done. A real scrypt on
// parameters that should have been refused could take gigabytes, which is the
// very thing being tested for, so it must not be what tells us. The result says
// whether scrypt was reached.
func forbidScrypt(t *testing.T) *bool {
	t.Helper()
	reached := new(bool)
	was := scryptKey
	scryptKey = func(_, _ []byte, _, _, _, _ int) ([]byte, error) {
		*reached = true
		return nil, errors.New("scrypt was reached")
	}
	t.Cleanup(func() { scryptKey = was })
	return reached
}

// withParams is file with the scrypt parameters in its header replaced.
func withParams(file []byte, k kdf) []byte {
	out := bytes.Clone(file)
	binary.BigEndian.PutUint32(out[paramsAt:], k.n)
	binary.BigEndian.PutUint32(out[paramsAt+4:], k.r)
	binary.BigEndian.PutUint32(out[paramsAt+8:], k.p)
	return out
}

func TestARoundTrip(t *testing.T) {
	snapshot := bytes.Clone(sample)
	file, err := Export(snapshot, pass)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(snapshot, sample) {
		t.Fatal("Export changed what it was given")
	}
	if len(file) != headerLen+len(sample)+tagLen || string(file[:8]) != "RNBACKUP" {
		t.Fatalf("a file of %d bytes starting %q", len(file), file[:8])
	}
	// it is encrypted: neither the text nor a secret in it can be read off the file
	for _, plain := range []string{"priv-secret", "reality_private_key", "version", "تهران"} {
		if bytes.Contains(file, []byte(plain)) {
			t.Errorf("%q is in the file as it is", plain)
		}
	}
	if k, err := readHeader(file); err != nil || k != defaultKDF {
		t.Fatalf("header says %+v, %v; want the default %+v", k, err, defaultKDF)
	}

	kept := bytes.Clone(file)
	got, err := Import(file, pass)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, sample) {
		t.Fatalf("got %q", got)
	}
	if !bytes.Equal(file, kept) {
		t.Fatal("Import changed the file it was given")
	}
	got[0] ^= 0xff // the result is the caller's, not a window into the file
	if !bytes.Equal(file, kept) {
		t.Fatal("the result of Import shares memory with the file")
	}
}

func TestALargeSnapshotRoundTrips(t *testing.T) {
	big := make([]byte, 4<<20+123)
	mrand.New(mrand.NewSource(1)).Read(big)
	file, err := Export(big, pass)
	if err != nil {
		t.Fatal(err)
	}
	if len(file) != headerLen+len(big)+tagLen {
		t.Fatalf("a snapshot of %d bytes became a file of %d", len(big), len(file))
	}
	got, err := Import(file, pass)
	if err != nil || !bytes.Equal(got, big) {
		t.Fatalf("round trip: equal = %v, err = %v", bytes.Equal(got, big), err)
	}
}

func TestNothingAndEmptyAreSnapshotsToo(t *testing.T) {
	for name, snapshot := range map[string][]byte{"nil": nil, "empty": {}, "one byte": {0}} {
		file := sealFast(t, snapshot)
		if len(file) != headerLen+len(snapshot)+tagLen {
			t.Errorf("%s: a file of %d bytes", name, len(file))
		}
		got, err := Import(file, pass)
		if err != nil || !bytes.Equal(got, snapshot) {
			t.Errorf("%s: got %q, %v", name, got, err)
		}
	}
}

func TestAWrongPassphraseDoesNotOpenIt(t *testing.T) {
	file := sealFast(t, sample)
	for _, wrong := range []string{
		"", "x", "short", "another passphrase", pass + " ", " " + pass, strings.ToUpper(pass),
		"correct horse battery stapl", "Correct horse battery staple",
	} {
		_, err := Import(file, wrong)
		if !errors.Is(err, ErrBadPassphrase) || errors.Is(err, ErrBadFile) || errors.Is(err, ErrWeakPassphrase) {
			t.Errorf("passphrase %q: err = %v, want ErrBadPassphrase and nothing else", wrong, err)
		}
	}
	if got, err := Import(file, pass); err != nil || !bytes.Equal(got, sample) {
		t.Fatalf("the right one: %q, %v", got, err)
	}
}

// Whatever byte of the file changes, it doesn't open: not the header, which
// GCM is told to check, nor the body or the tag.
func TestAnyChangedByteStopsTheFileOpening(t *testing.T) {
	file := sealFast(t, sample)
	kept := bytes.Clone(file)
	for i := range file {
		for _, bit := range []byte{0x01, 0x80} {
			bad := bytes.Clone(file)
			bad[i] ^= bit
			got, err := Import(bad, pass)
			if err == nil || got != nil {
				t.Fatalf("byte %d changed by %#x and the file opened", i, bit)
			}
			switch {
			case i < paramsAt: // the magic and the format
				if !errors.Is(err, ErrBadFile) {
					t.Errorf("byte %d: %v, want ErrBadFile", i, err)
				}
			case i < saltAt: // the scrypt parameters: caught as absurd, or they change the key
				if !errors.Is(err, ErrBadFile) && !errors.Is(err, ErrBadPassphrase) {
					t.Errorf("byte %d: %v", i, err)
				}
			default: // the salt, the nonce, the body and the tag
				if !errors.Is(err, ErrBadPassphrase) {
					t.Errorf("byte %d: %v, want ErrBadPassphrase", i, err)
				}
			}
		}
	}
	if !bytes.Equal(file, kept) {
		t.Fatal("the original changed")
	}
	if _, err := Import(file, pass); err != nil {
		t.Fatal(err)
	}
}

func TestACutOrLongerFileDoesNotOpen(t *testing.T) {
	file := sealFast(t, sample)
	for n := 0; n < len(file); n++ {
		got, err := Import(file[:n], pass)
		if got != nil {
			t.Fatalf("%d of %d bytes gave a snapshot", n, len(file))
		}
		switch {
		case n < headerLen+tagLen:
			// not even a header and a tag
			if !errors.Is(err, ErrBadFile) {
				t.Errorf("%d bytes: %v, want ErrBadFile", n, err)
			}
		default:
			// the tag is there but covers more than is left, which GCM can't tell from a wrong passphrase
			if !errors.Is(err, ErrBadPassphrase) {
				t.Errorf("%d bytes: %v, want ErrBadPassphrase", n, err)
			}
		}
	}
	for _, extra := range []string{"\n", "x", strings.Repeat("y", 100)} {
		if _, err := Import(append(bytes.Clone(file), extra...), pass); !errors.Is(err, ErrBadPassphrase) {
			t.Errorf("%d extra bytes: %v, want ErrBadPassphrase", len(extra), err)
		}
	}
}

func TestAFormatThisBuildDoesNotKnow(t *testing.T) {
	file := sealFast(t, sample)
	for _, v := range []byte{0, 2, 3, 127, 255} {
		bad := bytes.Clone(file)
		bad[versionAt] = v
		_, err := Import(bad, pass)
		if !errors.Is(err, ErrBadFile) {
			t.Errorf("format %d: %v, want ErrBadFile", v, err)
		}
	}
	// a newer release's file says so, which is more useful than "unknown"
	bad := bytes.Clone(file)
	bad[versionAt] = 2
	if _, err := Import(bad, pass); err == nil || !strings.Contains(err.Error(), "format 2") || !strings.Contains(err.Error(), "up to 1") {
		t.Errorf("format 2: %v", err)
	}
}

func TestSomethingThatIsNotABackup(t *testing.T) {
	file := sealFast(t, sample)
	wrongMagic := bytes.Clone(file)
	copy(wrongMagic, "RNBACKUQ")
	wrongCase := bytes.Clone(file)
	copy(wrongCase, "rnbackup")
	for name, data := range map[string][]byte{
		"nil":                         nil,
		"empty":                       {},
		"a different magic":           wrongMagic,
		"the magic in lower":          wrongCase,
		"the state file":              []byte(`{"version": 1, "nodes": [], "subscriptions": []}` + strings.Repeat(" ", 100)),
		"zeros":                       make([]byte, 200),
		"a PNG":                       append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 200)...),
		"the body without the header": file[headerLen:],
	} {
		if got, err := Import(data, pass); !errors.Is(err, ErrBadFile) || got != nil {
			t.Errorf("%s: %q, %v; want ErrBadFile", name, got, err)
		}
	}
}

// A crafted file must not be able to make the hub allocate a lot of memory or
// spin, so its parameters are held to a cap before scrypt is ever run.
func TestAskingForTooMuchIsRefusedBeforeAnyWork(t *testing.T) {
	file := sealFast(t, sample)
	reached := forbidScrypt(t)

	for _, k := range []kdf{
		{1 << 31, 8, 1}, {1 << 17, 8, 1}, {1<<16 + 1, 8, 1}, {1 << 15, 9, 1}, {1 << 15, 1 << 31, 1},
		{1 << 15, 8, 3}, {1 << 15, 8, 1 << 31}, {1 << 31, 1 << 31, 1 << 31}, {0xffffffff, 0xffffffff, 0xffffffff},
		// not usable by scrypt at all
		{0, 8, 1}, {1, 8, 1}, {3, 8, 1}, {1<<15 + 1, 8, 1}, {1 << 15, 0, 1}, {1 << 15, 8, 0},
	} {
		start := time.Now()
		got, err := Import(withParams(file, k), pass)
		if !errors.Is(err, ErrBadFile) || got != nil {
			t.Errorf("%+v: %q, %v; want ErrBadFile", k, got, err)
		}
		if d := time.Since(start); d > time.Second {
			t.Errorf("%+v took %v to refuse", k, d)
		}
	}
	// and Export won't make a file that the cap would turn away
	if _, err := seal(sample, pass, kdf{1 << 20, 8, 1}, rand.Reader); !errors.Is(err, ErrBadFile) {
		t.Errorf("sealing under parameters over the cap: %v", err)
	}
	if *reached {
		t.Fatal("scrypt ran for parameters that were refused")
	}
}

func TestWhatParametersMayBeAsked(t *testing.T) {
	for _, k := range []kdf{defaultKDF, {2, 1, 1}, {1 << 6, 8, 1}, {1 << 16, 8, 2}, {1 << 16, 1, 1}, {1 << 10, 4, 2}} {
		if err := k.check(); err != nil {
			t.Errorf("%+v: %v", k, err)
		}
	}
	// whatever Export makes, Import has to accept
	if defaultKDF.n > maxN || defaultKDF.r > maxR || defaultKDF.p > maxP {
		t.Errorf("the default %+v is over the cap", defaultKDF)
	}
	// and the cap keeps scrypt's memory (128*N*r) to 64 MiB
	if mem := 128 * uint64(maxN) * uint64(maxR); mem > 64<<20 {
		t.Errorf("the caps allow %d bytes", mem)
	}
}

// The parameters are in the file, so raising the default later leaves files
// made before it readable.
func TestTheFileSaysWhatItWasMadeWith(t *testing.T) {
	for _, k := range []kdf{{1 << 7, 8, 1}, {1 << 8, 4, 2}, {1 << 9, 1, 1}, {2, 1, 1}, {1 << 6, 8, 2}} {
		file, err := seal(sample, pass, k, rand.Reader)
		if err != nil {
			t.Fatalf("%+v: %v", k, err)
		}
		if got, err := readHeader(file); err != nil || got != k {
			t.Errorf("%+v: the header says %+v, %v", k, got, err)
		}
		if got, err := Import(file, pass); err != nil || !bytes.Equal(got, sample) {
			t.Errorf("%+v: %q, %v", k, got, err)
		}
		// opened under the parameters it was made with, and no others
		if _, err := Import(withParams(file, kdf{k.n * 2, k.r, k.p}), pass); !errors.Is(err, ErrBadPassphrase) && !errors.Is(err, ErrBadFile) {
			t.Errorf("%+v: a header that lies about N: %v", k, err)
		}
	}
}

// Export counts characters, not bytes: seven Persian letters are fourteen
// bytes and still too short.
func TestAShortPassphraseIsRefusedBeforeAnyWork(t *testing.T) {
	reached := forbidScrypt(t)
	for _, short := range []string{"", " ", "1", "1234567", "abcdefg", "اختلاف", "پسوردها", "7 chars", "ééééééé"} {
		file, err := Export(sample, short)
		if !errors.Is(err, ErrWeakPassphrase) || file != nil {
			t.Errorf("%q (%d characters): %v, want ErrWeakPassphrase", short, len([]rune(short)), err)
		}
	}
	if *reached {
		t.Fatal("scrypt ran for a passphrase that was refused")
	}
}

func TestPassphrasesFromEightCharactersUp(t *testing.T) {
	for _, ok := range []string{"12345678", "abcdefgh", "پسورد-امن", "éééééééé", "        ", strings.Repeat("x", 500)} {
		file, err := seal(sample, ok, fast, rand.Reader)
		if err != nil {
			t.Errorf("%q: %v", ok, err)
			continue
		}
		if got, err := Import(file, ok); err != nil || !bytes.Equal(got, sample) {
			t.Errorf("%q: %q, %v", ok, got, err)
		}
	}

	// Import never judges the length: the rule may change, and a file made
	// under the old one still has to open. A short passphrase is only a wrong one.
	file := sealFast(t, sample)
	for _, short := range []string{"", "x", "1234567"} {
		if _, err := Import(file, short); !errors.Is(err, ErrBadPassphrase) {
			t.Errorf("Import with %q: %v, want ErrBadPassphrase", short, err)
		}
	}
}

func TestTwoExportsOfTheSameInputDiffer(t *testing.T) {
	a, err := Export(sample, pass)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Export(sample, pass)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("two exports are the same file")
	}
	if bytes.Equal(a[saltAt:nonceAt], b[saltAt:nonceAt]) {
		t.Error("the same salt twice")
	}
	if bytes.Equal(a[nonceAt:headerLen], b[nonceAt:headerLen]) {
		t.Error("the same nonce twice")
	}
	if bytes.Equal(a[headerLen:], b[headerLen:]) {
		t.Error("the same body twice")
	}
	if !bytes.Equal(a[:saltAt], b[:saltAt]) {
		t.Error("the fixed part of the header differs")
	}
}

// Made by a program that shares nothing with this one (Python's hashlib.scrypt
// and cryptography's AESGCM), from the passphrase, salt, nonce and plaintext
// below. If either side of this test breaks, the format has changed and files
// already handed out won't open.
const (
	goldenPlain = `{"version":1,"note":"golden"}` + "\n"
	goldenHdr   = "524e4241434b5550" + // RNBACKUP
		"01" + // format 1
		"00008000" + // N = 1<<15
		"00000008" + // r
		"00000001" + // p
		"000102030405060708090a0b0c0d0e0f" + // salt
		"a0a1a2a3a4a5a6a7a8a9aaab" // nonce
	goldenBody = "669d381255f1c39a4487e10aee2917c1886701d927a68663991b63e3413c4361fd58eefca57f14a968172318cdd2"
)

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestTheHeaderIsAsWritten(t *testing.T) {
	fixed := bytes.NewReader(append(unhex(t, "000102030405060708090a0b0c0d0e0f"), unhex(t, "a0a1a2a3a4a5a6a7a8a9aaab")...))
	file, err := seal([]byte(goldenPlain), pass, defaultKDF, fixed)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(file[:headerLen]); got != goldenHdr {
		t.Fatalf("header\n got  %s\n want %s", got, goldenHdr)
	}
	if got := hex.EncodeToString(file[headerLen:]); got != goldenBody {
		t.Fatalf("body\n got  %s\n want %s", got, goldenBody)
	}
	if headerLen != 49 || len(file) != 49+len(goldenPlain)+16 {
		t.Fatalf("header is %d bytes, file %d", headerLen, len(file))
	}
}

func TestAFileFromAnotherImplementationOpens(t *testing.T) {
	got, err := Import(unhex(t, goldenHdr+goldenBody), pass)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != goldenPlain {
		t.Fatalf("got %q", got)
	}
}

// The key is wiped as soon as the cipher has it, on every way out, and so is
// the passphrase as bytes.
func TestTheKeyAndThePassphraseAreWiped(t *testing.T) {
	type seen struct{ pass, key, keyThen []byte }
	var calls []seen
	was := scryptKey
	scryptKey = func(pw, salt []byte, n, r, p, l int) ([]byte, error) {
		key, err := was(pw, salt, n, r, p, l)
		calls = append(calls, seen{pw, key, bytes.Clone(key)})
		return key, err
	}
	defer func() { scryptKey = was }()

	file := sealFast(t, sample) // one call
	if _, err := Import(file, pass); err != nil {
		t.Fatal(err)
	}
	if _, err := Import(file, "not the passphrase"); !errors.Is(err, ErrBadPassphrase) {
		t.Fatal(err)
	}
	if len(calls) != 3 {
		t.Fatalf("scrypt ran %d times, want 3", len(calls))
	}
	zero := func(b []byte) bool { return bytes.Equal(b, make([]byte, len(b))) }
	for i, c := range calls {
		if len(c.key) != 32 || zero(c.keyThen) {
			t.Fatalf("call %d: the key was %d bytes, all zero = %v; the test isn't looking at a key", i, len(c.key), zero(c.keyThen))
		}
		if !zero(c.key) {
			t.Errorf("call %d: the key was left in memory", i)
		}
		if len(c.pass) == 0 || !zero(c.pass) {
			t.Errorf("call %d: the passphrase as bytes was left in memory (%d bytes)", i, len(c.pass))
		}
	}
}
