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
	"strconv"
	"strings"
	"syscall"
	"time"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/signing"
	"github.com/sirupsen/logrus"
)

// SelfUpdate receives a new agent binary and updates in-place
func (s *Server) SelfUpdate(ctx context.Context, req *pb.UpdateRequest) (*pb.UpdateResponse, error) {
	log := logrus.WithFields(logrus.Fields{
		"new_version":   req.Version,
		"binary_size":   len(req.BinaryContent),
		"restart_after": req.RestartAfter,
	})
	log.Info("[SelfUpdate] Received update request")

	// Get current binary path
	currentBinary, err := os.Executable()
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

	// Verify Ed25519 signature if a public key is configured
	if len(signing.PublicKey) > 0 {
		if len(req.Signature) == 0 {
			log.Error("[SelfUpdate] Signature missing: update rejected")
			return &pb.UpdateResponse{
				Success:    false,
				Message:    "Binary signature is required but was not provided",
				OldVersion: Version,
			}, nil
		}
		if !signing.Verify(binaryContent, req.Signature, signing.PublicKey) {
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
	verifyCmd := exec.Command(tempPath, "--version")
	if output, err := verifyCmd.CombinedOutput(); err != nil {
		os.Remove(tempPath)
		return &pb.UpdateResponse{
			Success:    false,
			Message:    fmt.Sprintf("New binary verification failed: %v - %s", err, string(output)),
			OldVersion: Version,
		}, nil
	}
	log.Debug("[SelfUpdate] New binary verified")

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

// isNewerVersion returns true if the candidate version is newer than the current version.
// Both versions should follow semver format (e.g., "v1.2.3" or "1.2.3").
// Returns true (allow update) if either version cannot be parsed.
func isNewerVersion(current, candidate string) bool {
	parse := func(v string) (int, int, int, bool) {
		v = strings.TrimPrefix(v, "v")
		parts := strings.SplitN(v, ".", 3)
		if len(parts) != 3 {
			return 0, 0, 0, false
		}
		major, e1 := strconv.Atoi(parts[0])
		minor, e2 := strconv.Atoi(parts[1])
		// Handle patch with pre-release suffix (e.g., "1-rc1")
		patchStr := strings.SplitN(parts[2], "-", 2)[0]
		patch, e3 := strconv.Atoi(patchStr)
		if e1 != nil || e2 != nil || e3 != nil {
			return 0, 0, 0, false
		}
		return major, minor, patch, true
	}
	cMaj, cMin, cPat, cOk := parse(current)
	nMaj, nMin, nPat, nOk := parse(candidate)
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
