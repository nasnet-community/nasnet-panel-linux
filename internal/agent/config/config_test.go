package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefaultProfileIsFull(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Profile != ProfileFull || cfg.Profile.Managed() {
		t.Fatalf("default profile = %q, managed %v", cfg.Profile, cfg.Profile.Managed())
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadProfile(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want Profile
	}{
		{"no line", "listen_addr: \":9091\"\n", ProfileFull},
		{"empty file", "", ProfileFull},
		{"full", "profile: full\n", ProfileFull},
		{"managed", "profile: managed\n", ProfileManaged},
		{"managed among other keys", "listen_addr: \":9091\"\nprofile: managed\nxray:\n  api_addr: 127.0.0.1:1\n", ProfileManaged},
		{"capitalised", "profile: Managed\n", ProfileManaged},
		{"upper case", "profile: MANAGED\n", ProfileManaged},
		{"upper case full", "profile: FULL\n", ProfileFull},
		{"quoted with spaces", "profile: \"  managed \"\n", ProfileManaged},
		{"empty string", "profile: \"\"\n", ProfileFull},
		{"nothing after the key", "profile:\n", ProfileFull},
		{"tilde", "profile: ~\n", ProfileFull},
		{"null", "profile: null\n", ProfileFull},
	}
	for _, c := range cases {
		cfg, err := Load(writeConfig(t, c.yaml))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if cfg.Profile != c.want {
			t.Errorf("%s: profile = %q, want %q", c.name, cfg.Profile, c.want)
		}
		if cfg.Profile.Managed() != (c.want == ProfileManaged) {
			t.Errorf("%s: Managed() = %v", c.name, cfg.Profile.Managed())
		}
	}
}

func TestLoadWithoutAFileIsFull(t *testing.T) {
	for name, path := range map[string]string{
		"no path":      "",
		"missing file": filepath.Join(t.TempDir(), "nope.yaml"),
	} {
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if cfg.Profile != ProfileFull {
			t.Errorf("%s: profile = %q", name, cfg.Profile)
		}
	}
}

func TestLoadRejectsAnUnknownProfile(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"made up", "profile: strict\n", `unknown profile "strict"`},
		{"near miss", "profile: manage\n", `unknown profile "manage"`},
		{"plural", "profile: managed2\n", `unknown profile "managed2"`},
		{"two words", "profile: \"managed full\"\n", `unknown profile "managed full"`},
		{"number", "profile: 1\n", `unknown profile "1"`},
		{"bool", "profile: true\n", `unknown profile "true"`},
		{"quoted null", "profile: \"null\"\n", `unknown profile "null"`},
		{"a list", "profile: [managed]\n", "must be a name"},
		{"a map", "profile: {name: managed}\n", "must be a name"},
	}
	for _, c := range cases {
		cfg, err := Load(writeConfig(t, "listen_addr: \":9091\"\n"+c.yaml))
		if err == nil {
			t.Errorf("%s: loaded, profile %q", c.name, cfg.Profile)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q does not mention %q", c.name, err, c.want)
		}
		if !strings.Contains(err.Error(), "line 2") {
			t.Errorf("%s: error %q does not say which line", c.name, err)
		}
		if cfg != nil {
			t.Errorf("%s: a config came back with the error", c.name)
		}
	}
}

func TestParseProfile(t *testing.T) {
	for in, want := range map[string]Profile{
		"": ProfileFull, "full": ProfileFull, " Full\n": ProfileFull,
		"managed": ProfileManaged, "MANAGED": ProfileManaged, "\tManaged ": ProfileManaged,
	} {
		got, err := ParseProfile(in)
		if err != nil || got != want {
			t.Errorf("ParseProfile(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"x", "ful", "full managed", "managed,full", "mana ged"} {
		if got, err := ParseProfile(in); err == nil {
			t.Errorf("ParseProfile(%q) = %q, want an error", in, got)
		}
	}
}

// Nothing but the file picks a profile: an environment that says otherwise
// changes neither a managed nor a full agent.
func TestProfileIgnoresTheEnvironment(t *testing.T) {
	for _, name := range []string{"AGENT_PROFILE", "PROFILE", "NODE_PROFILE", "NASNET_AGENT_PROFILE", "XRAY_PROFILE"} {
		t.Setenv(name, "full")
	}
	cfg, err := Load(writeConfig(t, "profile: managed\n"))
	if err != nil || cfg.Profile != ProfileManaged {
		t.Fatalf("managed file under a full environment: %q, %v", cfg.Profile, err)
	}

	for _, name := range []string{"AGENT_PROFILE", "PROFILE", "NODE_PROFILE", "NASNET_AGENT_PROFILE", "XRAY_PROFILE"} {
		t.Setenv(name, "managed")
	}
	cfg, err = Load(writeConfig(t, "listen_addr: \":9091\"\n"))
	if err != nil || cfg.Profile != ProfileFull {
		t.Fatalf("plain file under a managed environment: %q, %v", cfg.Profile, err)
	}
}

func TestSaveKeepsTheProfile(t *testing.T) {
	path := writeConfig(t, "profile: managed\nlisten_addr: \":9091\"\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	// what the calls that rewrite the file do: change something, then Save
	cfg.Xray.APIAddr = "127.0.0.1:10999"
	cfg.NextListenAddr = ":9092"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "profile: managed") {
		t.Fatalf("the saved file lost the profile:\n%s", raw)
	}
	again, err := Load(path)
	if err != nil || again.Profile != ProfileManaged {
		t.Fatalf("reloaded profile = %q, %v", again.Profile, err)
	}
	if again.Xray.APIAddr != "127.0.0.1:10999" {
		t.Fatalf("the other change was lost: %q", again.Xray.APIAddr)
	}
}

// A full agent's file doesn't grow a line when something saves it.
func TestSaveLeavesAFullAgentsFileAlone(t *testing.T) {
	for name, content := range map[string]string{
		"no line":  "listen_addr: \":9091\"\n",
		"explicit": "profile: full\nlisten_addr: \":9091\"\n",
	} {
		path := writeConfig(t, content)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := cfg.Save(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		raw, _ := os.ReadFile(path)
		if strings.Contains(string(raw), "profile") {
			t.Errorf("%s: the saved file mentions a profile:\n%s", name, raw)
		}
		if again, err := Load(path); err != nil || again.Profile != ProfileFull {
			t.Errorf("%s: reloaded profile = %q, %v", name, again.Profile, err)
		}
	}
}

func TestValidateChecksTheProfile(t *testing.T) {
	for _, p := range []Profile{"", ProfileFull, ProfileManaged, "Managed", " FULL "} {
		cfg := DefaultConfig()
		cfg.Profile = p
		if err := cfg.Validate(); err != nil {
			t.Errorf("profile %q: %v", p, err)
		}
	}
	for _, p := range []Profile{"strict", "full,managed", "manage"} {
		cfg := DefaultConfig()
		cfg.Profile = p
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "unknown profile") {
			t.Errorf("profile %q: error = %v", p, err)
		}
	}
}

// A profile nobody parsed can only tighten the agent.
func TestAnUncheckedProfileCountsAsManaged(t *testing.T) {
	for p, want := range map[Profile]bool{
		"": false, ProfileFull: false,
		ProfileManaged: true, "Managed": true, "full ": true, "garbage": true,
	} {
		if p.Managed() != want {
			t.Errorf("Profile(%q).Managed() = %v, want %v", p, p.Managed(), want)
		}
	}
	if Profile("").String() != "full" || ProfileManaged.String() != "managed" {
		t.Fatalf("names: %q, %q", Profile("").String(), ProfileManaged.String())
	}
}
