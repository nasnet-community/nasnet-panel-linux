package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Profile is how much of its API the agent gives the hub. The config file sets
// it once at startup and no call can change it afterwards.
type Profile string

const (
	// ProfileFull keeps every call, as agents always have.
	ProfileFull Profile = "full"
	// ProfileManaged is for nodes run by a hub nobody logs in to: a short
	// allow-list of calls, and no shell, terminal, file-write call, mode
	// switch or uninstall. The hub can still push Xray configs and binaries.
	ProfileManaged Profile = "managed"
)

// ParseProfile reads a profile as the config file spells it. Case and
// surrounding spaces don't matter, and nothing at all means full.
func ParseProfile(s string) (Profile, error) {
	switch p := Profile(strings.ToLower(strings.TrimSpace(s))); p {
	case "", ProfileFull:
		return ProfileFull, nil
	case ProfileManaged:
		return ProfileManaged, nil
	}
	return "", fmt.Errorf("unknown profile %q (use %q or %q)", s, ProfileFull, ProfileManaged)
}

// Managed reports whether the restrictions apply. Anything that isn't plainly
// full counts, so a value that skipped ParseProfile can only tighten the agent.
func (p Profile) Managed() bool {
	return p != "" && p != ProfileFull
}

func (p Profile) String() string {
	if p == "" {
		return string(ProfileFull)
	}
	return string(p)
}

// IsZero keeps a full agent's config file as it is: Save leaves the default
// out instead of adding a line to every existing node.
func (p Profile) IsZero() bool {
	return p == "" || p == ProfileFull
}

// UnmarshalYAML rejects what ParseProfile does, so an unknown profile stops
// the agent at startup instead of quietly running as something else.
func (p *Profile) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: profile must be a name like %q", n.Line, ProfileManaged)
	}
	if n.ShortTag() == "!!null" { // "profile:" with nothing after it
		*p = ProfileFull
		return nil
	}
	v, err := ParseProfile(n.Value)
	if err != nil {
		return fmt.Errorf("line %d: %w", n.Line, err)
	}
	*p = v
	return nil
}
