package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/signing"
	"github.com/sirupsen/logrus"
)

// selfPath is the running binary; tests point it at a stand-in.
var selfPath = os.Executable

// SelfUpdate receives a new agent binary and updates in-place
func (s *Server) SelfUpdate(ctx context.Context, req *pb.UpdateRequest) (*pb.UpdateResponse, error) {
	log := logrus.WithFields(logrus.Fields{
		"new_version":   req.Version,
		"binary_size":   len(req.BinaryContent),
		"restart_after": req.RestartAfter,
	})
	log.Info("[SelfUpdate] Received update request")

	// Get current binary path
	currentBinary, err := selfPath()
	if err != nil {
		return &pb.UpdateResponse{
			Success:    false,
			Message:    fmt.Sprintf("Failed to get current binary path: %v", err),
			OldVersion: Version,
		}, nil
	}

	// Decompress binary if gzipped
	var binaryContent []byte
	if isGzipped(req.BinaryContent) {
		log.Debug("[SelfUpdate] Decompressing gzipped binary")
		binaryContent, err = decompressGzip(req.BinaryContent)
		if err != nil {
			return &pb.UpdateResponse{
				Success:    false,
				Message:    fmt.Sprintf("Failed to decompress binary: %v", err),
				OldVersion: Version,
			}, nil
		}
	} else {
		binaryContent = req.BinaryContent
	}

	// Verify checksum
	actualChecksum := sha256sum(binaryContent)
	if req.Checksum != "" && req.Checksum != actualChecksum {
		log.WithFields(logrus.Fields{
			"expected": req.Checksum,
			"actual":   actualChecksum,
		}).Error("[SelfUpdate] Checksum mismatch")
		return &pb.UpdateResponse{
			Success:    false,
			Message:    fmt.Sprintf("Checksum mismatch: expected %s, got %s", req.Checksum, actualChecksum),
			OldVersion: Version,
		}, nil
	}
	log.WithField("checksum", actualChecksum).Debug("[SelfUpdate] Checksum verified")

	// Verify Ed25519 signature if this build trusts a release key
	if len(signing.Trusted()) > 0 {
		if len(req.Signature) == 0 {
			log.Error("[SelfUpdate] Signature missing: update rejected")
			return &pb.UpdateResponse{
				Success:    false,
				Message:    "Binary signature is required but was not provided",
				OldVersion: Version,
			}, nil
		}
		if !signing.VerifyAny(binaryContent, req.Signature) {
			log.Error("[SelfUpdate] Signature verification failed: update rejected")
			return &pb.UpdateResponse{
				Success:    false,
				Message:    "Binary signature verification failed",
				OldVersion: Version,
			}, nil
		}
		log.Debug("[SelfUpdate] Ed25519 signature verified")
	}

	// Downgrade protection: reject older versions unless forced
	if req.Version != "" && Version != "dev" && !req.Force {
		if !isNewerVersion(Version, req.Version) {
			return &pb.UpdateResponse{
				Success:    false,
				Message:    fmt.Sprintf("Downgrade rejected: current=%s requested=%s (use force to override)", Version, req.Version),
				OldVersion: Version,
			}, nil
		}
	}

	// Write new binary to temp location
	tempPath := currentBinary + ".new"
	if err := os.WriteFile(tempPath, binaryContent, 0755); err != nil {
		return &pb.UpdateResponse{
			Success:    false,
			Message:    fmt.Sprintf("Failed to write new binary: %v", err),
			OldVersion: Version,
		}, nil
	}
	log.WithField("temp_path", tempPath).Debug("[SelfUpdate] New binary written")

	// Verify the new binary is executable
	versionOut, err := exec.Command(tempPath, "--version").CombinedOutput()
	if err != nil {
		os.Remove(tempPath)
		return &pb.UpdateResponse{
			Success:    false,
			Message:    fmt.Sprintf("New binary verification failed: %v - %s", err, string(versionOut)),
			OldVersion: Version,
		}, nil
	}
	log.Debug("[SelfUpdate] New binary verified")

	// An older release has no profile and would hand out a shell. The check
	// above trusts the version the hub names (or none), so a managed agent
	// reads it off the signed build itself, force or not.
	if s.profile.Managed() {
		if err := refuseOlderBuild(Version, string(versionOut)); err != nil {
			os.Remove(tempPath)
			log.WithError(err).Warn("[SelfUpdate] Update refused")
			return &pb.UpdateResponse{Success: false, Message: err.Error(), OldVersion: Version}, nil
		}
	}

	// Backup current binary
	backupPath := currentBinary + ".old"
	if err := os.Rename(currentBinary, backupPath); err != nil {
		os.Remove(tempPath)
		return &pb.UpdateResponse{
			Success:    false,
			Message:    fmt.Sprintf("Failed to backup current binary: %v", err),
			OldVersion: Version,
		}, nil
	}

	// Atomic rename new to current
	if err := os.Rename(tempPath, currentBinary); err != nil {
		// Try to restore backup and verify it succeeded
		if restoreErr := os.Rename(backupPath, currentBinary); restoreErr != nil {
			log.WithError(restoreErr).Error("[SelfUpdate] CRITICAL: Failed to restore backup after failed rename. Binary may be missing!")
			return &pb.UpdateResponse{
				Success:    false,
				Message:    fmt.Sprintf("Failed to replace binary: %v; CRITICAL: backup restore also failed: %v", err, restoreErr),
				OldVersion: Version,
			}, nil
		}
		// Verify the restored binary is executable
		if _, statErr := os.Stat(currentBinary); statErr != nil {
			log.WithError(statErr).Error("[SelfUpdate] CRITICAL: Restored binary not accessible after restore")
			return &pb.UpdateResponse{
				Success:    false,
				Message:    fmt.Sprintf("Failed to replace binary: %v; restored backup but verify failed: %v", err, statErr),
				OldVersion: Version,
			}, nil
		}
		return &pb.UpdateResponse{
			Success:    false,
			Message:    fmt.Sprintf("Failed to replace binary: %v (backup restored successfully)", err),
			OldVersion: Version,
		}, nil
	}

	// Clean up backup after successful replacement
	defer func() {
		if err := os.Remove(backupPath); err != nil {
			log.WithError(err).Debug("[SelfUpdate] Failed to clean up backup (non-critical)")
		}
	}()

	log.Info("[SelfUpdate] Binary successfully replaced")

	// Restart if requested
	if req.RestartAfter {
		log.Info("[SelfUpdate] Scheduling restart...")
		go func() {
			// Give enough time for the gRPC response to be fully sent and flushed
			time.Sleep(2 * time.Second)
			s.restartSelf(currentBinary)
		}()
	}

	return &pb.UpdateResponse{
		Success:    true,
		Message:    "Update successful, restarting...",
		OldVersion: Version,
		NewVersion: req.Version,
	}, nil
}

// restartSelf restarts the agent process
func (s *Server) restartSelf(binaryPath string) {
	log := logrus.WithField("binary", binaryPath)

	// Try to get systemd to restart us
	if runtime.GOOS == "linux" {
		// Check if we're running under systemd
		if _, err := os.Stat("/run/systemd/system"); err == nil {
			log.Info("[SelfUpdate] Requesting systemd restart")
			// Signal systemd to restart us
			cmd := exec.Command("systemctl", "restart", "nasnet-agent")
			if err := cmd.Run(); err != nil {
				log.WithError(err).Warn("[SelfUpdate] systemd restart failed, trying exec")
			} else {
				return
			}
		}
	}

	// Direct exec (for non-systemd or fallback)
	log.Info("[SelfUpdate] Performing exec restart")
	args := os.Args
	env := os.Environ()

	// Stop current operations gracefully
	// Create a context with timeout for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.Stop(ctx)

	// Replace ourselves with the new binary
	if err := syscall.Exec(binaryPath, args, env); err != nil {
		log.WithError(err).Error("[SelfUpdate] Failed to exec new binary")
		// Last resort: just exit, let systemd restart us
		os.Exit(1)
	}
}

// isGzipped checks if data is gzip compressed
func isGzipped(data []byte) bool {
	return len(data) > 2 && data[0] == 0x1f && data[1] == 0x8b
}

// maxDecompressedSize is the maximum allowed size for a decompressed binary (500MB)
const maxDecompressedSize = 500 * 1024 * 1024

// decompressGzip decompresses gzip data with a size limit to prevent zip bombs
func decompressGzip(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	limitedReader := io.LimitReader(reader, maxDecompressedSize+1)
	decompressed, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, err
	}
	if len(decompressed) > maxDecompressedSize {
		return nil, fmt.Errorf("decompressed size exceeds limit of %d bytes", maxDecompressedSize)
	}
	return decompressed, nil
}

// sha256sum calculates SHA256 checksum
func sha256sum(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// parseVersion reads "v1.2.3", "1.2.3" or "1.2.3-rc1" into its numbers.
func parseVersion(v string) (major, minor, patch int, ok bool) {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	ma, e1 := strconv.Atoi(parts[0])
	mi, e2 := strconv.Atoi(parts[1])
	// Handle patch with pre-release suffix (e.g., "1-rc1")
	patchStr := strings.SplitN(parts[2], "-", 2)[0]
	pa, e3 := strconv.Atoi(patchStr)
	if e1 != nil || e2 != nil || e3 != nil {
		return 0, 0, 0, false
	}
	return ma, mi, pa, true
}

// isNewerVersion returns true if the candidate version is newer than the current version.
// Both versions should follow semver format (e.g., "v1.2.3" or "1.2.3").
// Returns true (allow update) if either version cannot be parsed.
func isNewerVersion(current, candidate string) bool {
	cMaj, cMin, cPat, cOk := parseVersion(current)
	nMaj, nMin, nPat, nOk := parseVersion(candidate)
	if !cOk || !nOk {
		return true // Can't parse → allow update
	}
	if nMaj != cMaj {
		return nMaj > cMaj
	}
	if nMin != cMin {
		return nMin > cMin
	}
	return nPat > cPat
}

// reportedVersion is the version a build prints for --version: the last word
// of the first line, as in "nasnet-agent version v1.3.0".
func reportedVersion(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			return f[len(f)-1]
		}
	}
	return ""
}

// refuseOlderBuild is the managed profile's downgrade check. It compares the
// version the new build reports with the running one, so neither the version
// the hub names nor force gets a say. A running "dev" build has nothing to
// compare against.
func refuseOlderBuild(current, versionOutput string) error {
	if current == "dev" {
		return nil
	}
	cMaj, cMin, cPat, ok := parseVersion(current)
	if !ok {
		return nil // an unreleased build of our own, nothing to hold it to
	}
	reported := reportedVersion(versionOutput)
	nMaj, nMin, nPat, ok := parseVersion(reported)
	if !ok {
		return fmt.Errorf("this agent runs the managed profile: can't tell which version the new build is (it says %q)", reported)
	}
	if slices.Compare([]int{nMaj, nMin, nPat}, []int{cMaj, cMin, cPat}) < 0 {
		return fmt.Errorf("this agent runs the managed profile: it won't go back to an older build (this is %s, the new one says %s)", current, reported)
	}
	return nil
}

// GetSelfChecksum returns the SHA256 checksum of the running binary
func (s *Server) GetSelfChecksum(ctx context.Context, req *pb.Empty) (*pb.ChecksumResponse, error) {
	// Get current binary path
	currentBinary, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get current binary path: %w", err)
	}

	// Read binary file
	content, err := os.ReadFile(currentBinary)
	if err != nil {
		return nil, fmt.Errorf("failed to read binary file: %w", err)
	}

	// Calculate checksum
	checksum := sha256sum(content)

	return &pb.ChecksumResponse{
		Checksum: checksum,
		Path:     currentBinary,
	}, nil
}
