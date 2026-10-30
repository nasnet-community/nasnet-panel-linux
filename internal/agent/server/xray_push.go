package server

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// maxXrayUnpacked caps what a pushed build may unpack to. Real builds are
	// about 30 MB; the rest is room to grow.
	maxXrayUnpacked = 64 << 20
	// maxXrayChunk is the most a data message may carry. The hub's client cuts
	// at the same size.
	maxXrayChunk = 1 << 20
	// xrayInputSlack lets the packed stream run a little past the unpacked cap,
	// as framing can make it. Beyond that the input itself is too much.
	xrayInputSlack = 1 << 20
)

var errXrayTooLarge = errors.New("xray push over the size cap")

// xrayPushStream is the receiving end of a PushXrayBinary call.
type xrayPushStream interface {
	Recv() (*pb.XrayBinaryChunk, error)
}

// PushXrayBinary installs an xray build the hub sends over the call. The
// stream is spooled into the xray directory and checked (size cap, SHA256, ELF
// and architecture) before xray is touched, so a bad push never costs a restart.
func (s *Server) PushXrayBinary(stream pb.NodeAgent_PushXrayBinaryServer) error {
	hdr, err := readXrayPushHeader(stream)
	if err != nil {
		return err
	}
	log := logrus.WithFields(logrus.Fields{"version": hdr.Version, "gzip": hdr.Gzip, "restart_after": hdr.RestartAfter})
	log.Info("[PushXrayBinary] Receiving xray build")

	spool, size, sum, err := spoolXrayPush(stream, hdr.Gzip, filepath.Dir(s.xrayMgr.BinaryPath()), maxXrayUnpacked)
	if err != nil {
		log.WithError(err).Warn("[PushXrayBinary] Push refused")
		return err
	}
	defer func() { _ = os.Remove(spool) }()

	if want := strings.ToLower(hdr.Sha256); sum != want {
		log.Warn("[PushXrayBinary] Checksum mismatch")
		return status.Errorf(codes.InvalidArgument, "checksum mismatch: expected %.16s..., got %.16s...", want, sum)
	}
	log.WithField("size", size).Info("[PushXrayBinary] Checksum verified")

	f, err := os.Open(spool)
	if err != nil {
		return status.Errorf(codes.Internal, "cannot reopen the spooled build: %v", err)
	}
	defer f.Close()

	// Check it here as well as in install: a build for the wrong host should be
	// refused before xray is stopped, not after.
	head := make([]byte, 64)
	n, _ := f.ReadAt(head, 0)
	if err := s.checkXrayBinary(head[:n]); err != nil {
		log.WithError(err).Warn("[PushXrayBinary] Build refused")
		return status.Errorf(codes.InvalidArgument, "refusing to install: %v", err)
	}

	// A hub that has given up on the call shouldn't find xray restarted.
	if err := stream.Context().Err(); err != nil {
		return status.FromContextError(err).Err()
	}
	return stream.SendAndClose(s.installPushed(f, sum, hdr))
}

// installPushed swaps the spooled build in, unless that very build is already
// installed.
func (s *Server) installPushed(build io.Reader, sum string, hdr *pb.XrayBinaryHeader) *pb.CommandResponse {
	s.xrayUpdateMu.Lock()
	defer s.xrayUpdateMu.Unlock()

	if s.xrayHasBuild(sum, hdr.Version) {
		logrus.WithField("version", hdr.Version).Info("[PushXrayBinary] This build is already installed")
		return &pb.CommandResponse{
			Success: true,
			Message: fmt.Sprintf("Xray %s is already installed", hdr.Version),
		}
	}

	sw, failed := s.beginXraySwap()
	if failed != nil {
		return failed
	}
	return sw.install(build, hdr.RestartAfter)
}

// xrayHasBuild reports whether the installed xray is the build with this
// SHA256 and says it is this version, so a push has nothing left to do.
// Caller must hold xrayUpdateMu.
func (s *Server) xrayHasBuild(sum, version string) bool {
	f, err := os.Open(s.xrayMgr.BinaryPath())
	if err != nil {
		return false
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil || hex.EncodeToString(h.Sum(nil)) != sum {
		return false
	}
	return reportsVersion(s.fetchXrayVersion(), version)
}

// reportsVersion reports whether the first line of `xray version`
// ("Xray 26.10.21 (Xray, Penetrates Everything.) ...") names this version.
func reportsVersion(line, version string) bool {
	for _, field := range strings.Fields(line) {
		if strings.TrimPrefix(field, "v") == version {
			return true
		}
	}
	return false
}

// readXrayPushHeader takes the first message of a push, which has to be a
// header with a usable version and checksum.
func readXrayPushHeader(stream xrayPushStream) (*pb.XrayBinaryHeader, error) {
	msg, err := stream.Recv()
	if err == io.EOF {
		return nil, status.Error(codes.InvalidArgument, "the call ended before the header")
	}
	if err != nil {
		return nil, err
	}
	hdr := msg.GetHeader()
	switch {
	case hdr == nil:
		return nil, status.Error(codes.InvalidArgument, "the first message must be the header")
	case hdr.Version == "":
		return nil, status.Error(codes.InvalidArgument, "version cannot be empty")
	case !validXrayVersion(hdr.Version):
		return nil, status.Errorf(codes.InvalidArgument, "invalid version format: %s", hdr.Version)
	case hdr.Sha256 == "":
		return nil, status.Error(codes.InvalidArgument, "sha256 is required: refusing to install an unverified binary")
	}
	if raw, err := hex.DecodeString(hdr.Sha256); err != nil || len(raw) != sha256.Size {
		return nil, status.Error(codes.InvalidArgument, "sha256 must be 64 hex characters")
	}
	return hdr, nil
}

// xrayChunks reads the data messages of a push as one byte stream.
type xrayChunks struct {
	stream xrayPushStream
	buf    []byte
	got    int64 // bytes taken off the wire so far
	limit  int64
}

func (c *xrayChunks) Read(p []byte) (int, error) {
	for len(c.buf) == 0 {
		msg, err := c.stream.Recv()
		if err != nil {
			return 0, err // io.EOF is the end of the data
		}
		data, ok := msg.GetPayload().(*pb.XrayBinaryChunk_Data)
		if !ok {
			return 0, status.Error(codes.InvalidArgument, "every message after the header must carry data")
		}
		if len(data.Data) > maxXrayChunk {
			return 0, status.Errorf(codes.InvalidArgument, "a data message is over %d bytes", maxXrayChunk)
		}
		if c.got += int64(len(data.Data)); c.got > c.limit {
			return 0, errXrayTooLarge
		}
		c.buf = data.Data
	}
	n := copy(p, c.buf)
	c.buf = c.buf[n:]
	return n, nil
}

// spoolXrayPush writes the push's unpacked bytes to a temp file in dir and
// returns its name, size and SHA256. It gives up past limit unpacked bytes, so
// a small gzip can't fill the disk. The temp file is gone again whenever it
// returns an error.
func spoolXrayPush(stream xrayPushStream, gz bool, dir string, limit int64) (string, int64, string, error) {
	f, err := os.CreateTemp(dir, ".xray-push-*")
	if err != nil {
		return "", 0, "", status.Errorf(codes.Internal, "cannot spool the build into %s: %v", dir, err)
	}
	size, sum, err := fillXraySpool(f, stream, gz, limit)
	if cerr := f.Close(); err == nil && cerr != nil {
		err = status.Errorf(codes.Internal, "cannot write the build: %v", cerr)
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", 0, "", err
	}
	return f.Name(), size, sum, nil
}

// fillXraySpool copies the push's data into f, unpacking it if asked, and
// hashes what it writes.
func fillXraySpool(f *os.File, stream xrayPushStream, gz bool, limit int64) (int64, string, error) {
	var src io.Reader = &xrayChunks{stream: stream, limit: limit + xrayInputSlack}
	if gz {
		zr, err := gzip.NewReader(src)
		if err != nil {
			return 0, "", xrayPushReadError(err, gz, limit)
		}
		defer zr.Close()
		src = zr
	}

	h := sha256.New()
	buf := make([]byte, 256<<10)
	var size int64
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			if size += int64(n); size > limit {
				return 0, "", xrayPushReadError(errXrayTooLarge, gz, limit)
			}
			if _, err := f.Write(buf[:n]); err != nil {
				if errors.Is(err, syscall.ENOSPC) {
					return 0, "", status.Error(codes.ResourceExhausted, "no space left on the node for the build")
				}
				return 0, "", status.Errorf(codes.Internal, "cannot write the build: %v", err)
			}
			h.Write(buf[:n])
		}
		if rerr == io.EOF {
			return size, hex.EncodeToString(h.Sum(nil)), nil
		}
		if rerr != nil {
			return 0, "", xrayPushReadError(rerr, gz, limit)
		}
	}
}

// xrayPushReadError turns what went wrong while reading the data into the
// status the hub sees.
func xrayPushReadError(err error, gz bool, limit int64) error {
	if _, ok := status.FromError(err); ok {
		return err // the call itself failed, or the hub broke the protocol
	}
	switch {
	case errors.Is(err, errXrayTooLarge):
		return status.Errorf(codes.ResourceExhausted, "the build is over the %d byte limit once unpacked", limit)
	case gz:
		return status.Errorf(codes.InvalidArgument, "the data is not a complete gzip stream: %v", err)
	}
	return status.Errorf(codes.Internal, "cannot read the build: %v", err)
}
