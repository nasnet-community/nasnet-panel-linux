package depinstall

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/coredeps"
)

const (
	maxArtifactBytes = 300 << 20 // 300 MiB ceiling on any single download
	jobTimeout       = 30 * time.Minute
	downloadTimeout  = 10 * time.Minute
)

// run executes every step of a job in order, probing first (skip satisfied).
// Optional steps never fail the job.
func (m *Manager) run(parent context.Context, job *Job) {
	ctx, cancel := context.WithTimeout(parent, jobTimeout)
	defer cancel()

	job.setStatus(StatusRunning)

	if job.req.Uninstall {
		job.logf("uninstall started (%d steps)", len(job.steps))
		for _, step := range job.steps {
			m.uninstallStep(job, step)
		}
		job.finish(m.now())
		job.logf("uninstall finished: %s", job.snapshot(0).Status)
		return
	}

	job.logf("install started (%d steps)", len(job.steps))
	for _, step := range job.steps {
		if p, ok := step.dep.Probe.Resolve(); ok {
			job.setStep(step, StepSkipped, "already present")
			job.logf("[%s] %s already present at %s — skipping", step.CoreType, step.Name, p)
			continue
		}

		job.setStep(step, StepRunning, "")
		job.logf("[%s] installing %s…", step.CoreType, step.Name)

		var err error
		switch step.dep.Kind {
		case coredeps.KindBinary:
			err = m.installBinary(ctx, job, step)
		default:
			err = fmt.Errorf("unknown dependency kind %q", step.dep.Kind)
		}

		switch {
		case err == nil:
			job.setStep(step, StepOK, "installed")
			job.logf("[%s] %s ✓", step.CoreType, step.Name)
		case step.dep.Optional:
			job.setStep(step, StepSkipped, "optional: "+err.Error())
			job.logf("[%s] %s optional — continuing after error: %v", step.CoreType, step.Name, err)
		default:
			job.setStep(step, StepFailed, err.Error())
			job.logf("[%s] %s FAILED: %v", step.CoreType, step.Name, err)
		}
	}
	job.finish(m.now())
	job.logf("install finished: %s", job.snapshot(0).Status)
}

// installBinary downloads the pinned release tarball for this host, refuses it
// unless its SHA256 matches the manifest, extracts the binary and moves it into
// place atomically.
func (m *Manager) installBinary(ctx context.Context, job *Job, step *Step) error {
	spec := step.dep.Binary
	if spec == nil {
		return fmt.Errorf("no binary spec for %s", step.dep.Name)
	}
	link, want, err := spec.Target("")
	if err != nil {
		return err
	}
	job.logf("downloading %s", link)
	data, err := fetch(ctx, m.client, link)
	if err != nil {
		return fmt.Errorf("download %s: %w", link, err)
	}
	if err := verifySHA(job, step.dep.Name, data, want); err != nil {
		return err
	}
	bin, err := extractTarGzMember(data, spec.ArchiveMember)
	if err != nil {
		return fmt.Errorf("extract %q: %w", spec.ArchiveMember, err)
	}

	mode := spec.Mode
	if mode == 0 {
		mode = 0o755
	}
	if err := os.MkdirAll(filepath.Dir(spec.Dest), 0o755); err != nil {
		return err
	}
	tmp := spec.Dest + ".tmp"
	if err := os.WriteFile(tmp, bin, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, spec.Dest); err != nil { // atomic install
		_ = os.Remove(tmp)
		return err
	}
	job.logf("installed %s → %s", step.dep.Name, spec.Dest)
	return nil
}

// uninstallStep removes one binary the installer placed; one found elsewhere
// (a distro package, the offline bundle) is left alone.
func (m *Manager) uninstallStep(job *Job, step *Step) {
	spec := step.dep.Binary
	if spec == nil {
		job.setStep(step, StepSkipped, "nothing to remove")
		return
	}
	if _, err := os.Stat(spec.Dest); os.IsNotExist(err) {
		if p, ok := step.dep.Probe.Resolve(); ok {
			job.setStep(step, StepSkipped, "not installed by the panel ("+p+")")
			job.logf("[%s] %s at %s was not installed by the panel — left in place", step.CoreType, step.Name, p)
			return
		}
		job.setStep(step, StepSkipped, "not installed")
		job.logf("[%s] %s not installed — nothing to remove", step.CoreType, step.Name)
		return
	}
	job.setStep(step, StepRunning, "")
	if err := os.Remove(spec.Dest); err != nil {
		job.setStep(step, StepFailed, err.Error())
		job.logf("[%s] remove %s FAILED: %v", step.CoreType, step.Name, err)
		return
	}
	job.setStep(step, StepOK, "removed")
	job.logf("[%s] %s removed (%s) ✓", step.CoreType, step.Name, spec.Dest)
}

// ---- helpers ----

func fetch(ctx context.Context, client *http.Client, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxArtifactBytes))
}

// extractTarGzMember returns the bytes of the first regular file in a gzip'd tar
// whose basename equals member (e.g. "telemt" from telemt-aarch64-linux-musl.tar.gz).
func extractTarGzMember(data []byte, member string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar: %w", err)
		}
		if hdr.Typeflag == tar.TypeReg && path.Base(hdr.Name) == member {
			b, err := io.ReadAll(io.LimitReader(tr, maxArtifactBytes))
			if err != nil {
				return nil, err
			}
			if len(b) == 0 {
				return nil, fmt.Errorf("member %q is empty", member)
			}
			return b, nil
		}
	}
	return nil, fmt.Errorf("member %q not found in archive", member)
}

// verifySHA refuses data whose SHA256 does not match the pinned value.
func verifySHA(job *Job, name string, data []byte, want string) error {
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("%s sha256 mismatch: got %s, want %s", name, got, want)
	}
	job.logf("%s sha256 verified", name)
	return nil
}
