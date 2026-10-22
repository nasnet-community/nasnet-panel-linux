package usecase

import (
	"os"
	"path/filepath"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/release"
)

// Install kinds. Only a systemd release install updates itself.
const (
	KindSystemd = "systemd"
	KindDocker  = "docker"
	KindSource  = "source"
	KindNotRoot = "not_root"
)

// Kind is how this panel was installed and, when it can't update itself,
// what to run instead.
type Kind struct {
	Name    string `json:"name"`
	Why     string `json:"why,omitempty"`
	Command string `json:"command,omitempty"`
}

// host is what the kind is read from; tests fill it in by hand.
type host struct {
	docker     bool
	version    string
	euid       int
	invocation string // INVOCATION_ID, set by systemd
	unit       bool   // the panel's unit exists, for the CLI run from a shell
	exe        string
	root       string
}

func thisHost(version, root string) host {
	_, err := os.Stat("/.dockerenv")
	exe, _ := os.Executable()
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	return host{
		docker:     err == nil,
		version:    version,
		euid:       os.Geteuid(),
		invocation: os.Getenv("INVOCATION_ID"),
		unit:       exists("/etc/systemd/system/" + panelUnit),
		exe:        exe,
		root:       root,
	}
}

func kindOf(h host) Kind {
	if h.docker {
		return Kind{Name: KindDocker, Why: "Docker installs update through Docker.",
			Command: "git pull && docker compose up -d --build"}
	}
	if _, err := release.Parse(h.version); err != nil {
		return Kind{Name: KindSource, Why: "This panel was built from source.", Command: "sudo nasnet"}
	}
	if h.euid != 0 {
		return Kind{Name: KindNotRoot, Why: "The panel doesn't run as root yet. Auto-Update in the menu switches it over.",
			Command: "sudo nasnet"}
	}
	want := filepath.Join(h.root, "bin", "nasnet-panel")
	if p, err := filepath.EvalSymlinks(want); err == nil {
		want = p
	}
	if (h.invocation == "" && !h.unit) || h.exe != want {
		return Kind{Name: KindSource, Why: "This panel isn't running from its systemd service.", Command: "sudo nasnet"}
	}
	return Kind{Name: KindSystemd}
}
