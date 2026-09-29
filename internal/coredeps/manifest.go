package coredeps

// telemtVersion is the pinned MTProto relay release. The musl builds run on
// the fork's alpine image and on glibc hosts alike; the hashes are the
// release's own .sha256 sidecars, checked against the downloaded tarballs.
const telemtVersion = "3.4.25"

// TelemtPaths are where the relay may be installed, in lookup order after
// $PATH: the installer's destination, the distro location and the offline
// bundle.
var TelemtPaths = []string{"/usr/local/bin/telemt", "/usr/bin/telemt", "/usr/local/nasnet-panel/bin/telemt"}

// manifest is the source of truth: protocol → ordered dependencies.
var manifest = map[Protocol][]Dependency{
	// Pure-Go server — nothing to install.
	SSH: {},

	// Userspace TCP relay; one static binary.
	MTProto: {
		{
			Name:  "telemt",
			Kind:  KindBinary,
			Probe: Probe{Binary: "telemt", Paths: TelemtPaths},
			Binary: &BinarySpec{
				URLTemplate: "https://github.com/telemt/telemt/releases/download/" + telemtVersion + "/telemt-{uarch}-linux-musl.tar.gz",
				SHA256: map[string]string{
					"x86_64":  "33e21f17d6b54111ac9d53d85d54f191cf1e6c48cc9234ce05de5fe1bd8c5de8",
					"aarch64": "7c9a2196418bc9ecc8c7e493e9cfb299410cbbc065920fe3115ba08109118eed",
				},
				ArchiveMember: "telemt",
				Dest:          "/usr/local/bin/telemt",
				Mode:          0o755,
				Unsupported:   "MTProto needs an x86_64 or arm64 Linux box",
			},
		},
	},
}

// For returns the ordered dependency list for a protocol (nil if unknown).
func For(p Protocol) []Dependency {
	return manifest[p]
}

// Valid reports whether p is a protocol known to the manifest.
func (p Protocol) Valid() bool {
	_, ok := manifest[p]
	return ok
}

// Protocols returns all protocols known to the manifest in display order.
func Protocols() []Protocol {
	return []Protocol{SSH, MTProto}
}

// RequiresInstall reports whether a protocol has anything a node might be
// missing. SSH never does.
func RequiresInstall(p Protocol) bool {
	return len(manifest[p]) > 0
}
