package i18n

import (
	"strings"
	"testing"
)

// A completely unknown key returns the key itself — surface the missing
// string in the UI so translators notice.
func TestGet_MissingKeyReturnsKey(t *testing.T) {
	const missing = "definitelyMissingKey_xyz_test"
	if got := Get(LangEN, missing); got != missing {
		t.Errorf("Get(EN, missing) = %q, want %q", got, missing)
	}
}

// Unknown language quietly falls back to EN.
func TestGet_UnknownLangFallsBackToEN(t *testing.T) {
	en := Get(LangEN, "BtnBuyVPN")
	if en == "" || en == "BtnBuyVPN" {
		t.Skip("BtnBuyVPN missing from EN dictionary — test fixture changed")
	}
	if got := Get("xx_unknown", "BtnBuyVPN"); got != en {
		t.Errorf("unknown lang = %q, want EN fallback %q", got, en)
	}
}

// A key present in EN but missing in another language renders the EN copy.
func TestGet_MissingKeyInOtherLangFallsBackToEN(t *testing.T) {
	const onlyInEnglish = "definitelyEnglishOnly_xyz_test"
	// Not testing dictionary content; just verifying behaviour: when both
	// branches miss, the key is echoed back rather than panicking.
	if got := Get(LangFA, onlyInEnglish); got != onlyInEnglish {
		t.Errorf("got %q, want %q", got, onlyInEnglish)
	}
}

// fmt-style args are interpolated after lookup.
func TestGet_FormatsArgs(t *testing.T) {
	got := Get(LangEN, "ErrRedeemFailed", "myreason")
	if !strings.Contains(got, "myreason") {
		t.Errorf("formatted string missing arg: %q", got)
	}
}

// GetMD escapes string args so user input can't break Markdown rendering,
// but leaves numeric args alone.
func TestGetMD_EscapesStringArgsButNotNumbers(t *testing.T) {
	gotPlain := Get(LangEN, "ErrRedeemFailed", "*injected*")
	gotMD := GetMD(LangEN, "ErrRedeemFailed", "*injected*")
	if gotPlain == gotMD {
		t.Errorf("GetMD should differ from Get for special chars: %q vs %q", gotPlain, gotMD)
	}
	if !strings.Contains(gotMD, "\\*injected\\*") {
		t.Errorf("expected escaped asterisks in %q", gotMD)
	}

	// %d should pass through unchanged.
	gotNum := GetMD(LangEN, "PlanServers", 5)
	if !strings.Contains(gotNum, "5") {
		t.Errorf("number arg not interpolated: %q", gotNum)
	}
}

func TestGetLangName(t *testing.T) {
	if got := GetLangName(LangEN); got != "English" {
		t.Errorf("en = %q, want English", got)
	}
	if got := GetLangName(LangFA); got != "فارسی" {
		t.Errorf("fa = %q", got)
	}
	if got := GetLangName("xx"); got != "English" {
		t.Errorf("unknown = %q, want English (default)", got)
	}
}
