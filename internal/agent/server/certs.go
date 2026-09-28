package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/certs"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// maxInspectBytes caps how much of a certificate file InspectCertFile reads.
const maxInspectBytes = 1 << 20

// SetOnCertInstalled registers a hook that runs after InstallCertificate
// succeeds. It runs in its own goroutine.
func (s *Server) SetOnCertInstalled(fn func()) {
	s.onCertInstalled = fn
}

// verifyHubClient only lets the hub in, and applies the revocation denylist.
func (s *Server) verifyHubClient(cs tls.ConnectionState) error {
	if len(cs.PeerCertificates) == 0 {
		return errors.New("client certificate required")
	}
	if err := certs.RejectNodeClient(cs.PeerCertificates[0]); err != nil {
		return err
	}
	s.denylistMu.RLock()
	defer s.denylistMu.RUnlock()
	for _, c := range cs.PeerCertificates {
		if s.deniedSerials[c.SerialNumber.String()] {
			return fmt.Errorf("certificate with serial %s has been revoked", c.SerialNumber)
		}
	}
	return nil
}

// InstallCertificate replaces the agent's own certificate and key.
func (s *Server) InstallCertificate(_ context.Context, req *pb.InstallCertificateRequest) (*pb.InstallCertificateResponse, error) {
	s.installMu.Lock()
	defer s.installMu.Unlock()

	pair, err := tls.X509KeyPair(req.GetCertPem(), req.GetKeyPem())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "certificate and key do not match: %v", err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "parse certificate: %v", err)
	}

	caPEM, err := os.ReadFile(s.cfg.TLS.CACert)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "read CA: %v", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, status.Error(codes.FailedPrecondition, "agent CA file holds no certificate")
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "certificate is not valid for this node's CA: %v", err)
	}

	want, err := s.currentNodeID()
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "%v", err)
	}
	got, ok := certs.NodeIDFromCommonName(leaf.Subject.CommonName)
	if !ok || got != want {
		return nil, status.Errorf(codes.InvalidArgument, "certificate %q is for another node (this is node %d)", leaf.Subject.CommonName, want)
	}

	if err := writeCertPair(s.cfg.TLS.ServerCert, s.cfg.TLS.ServerKey, req.GetCertPem(), req.GetKeyPem()); err != nil {
		return nil, status.Errorf(codes.Internal, "write certificate: %v", err)
	}
	pair.Leaf = leaf
	s.serverCert.Store(&pair)

	logrus.WithFields(logrus.Fields{
		"serial":    leaf.SerialNumber.String(),
		"not_after": leaf.NotAfter,
	}).Info("[InstallCertificate] Agent certificate rotated")

	if s.onCertInstalled != nil {
		go s.onCertInstalled()
	}
	return &pb.InstallCertificateResponse{
		Serial:       leaf.SerialNumber.String(),
		NotAfterUnix: leaf.NotAfter.Unix(),
	}, nil
}

// currentNodeID reads the node ID from the certificate the agent runs with.
// Reverse mode never loads the listener, so fall back to the file.
func (s *Server) currentNodeID() (uint, error) {
	var leaf *x509.Certificate
	if c := s.serverCert.Load(); c != nil && c.Leaf != nil {
		leaf = c.Leaf
	} else {
		pemBytes, err := os.ReadFile(s.cfg.TLS.ServerCert)
		if err != nil {
			return 0, fmt.Errorf("read current certificate: %w", err)
		}
		if leaf, err = certs.ParseCertificate(pemBytes); err != nil {
			return 0, fmt.Errorf("parse current certificate: %w", err)
		}
	}
	id, ok := certs.NodeIDFromCommonName(leaf.Subject.CommonName)
	if !ok || id == 0 {
		return 0, fmt.Errorf("current certificate %q carries no node ID", leaf.Subject.CommonName)
	}
	return id, nil
}

// writeCertPair swaps in a new cert/key and keeps the old pair as .prev. Both
// temp files are written before anything is renamed, and a failed cert rename
// puts the old key back, so the key is never newer than the cert on disk.
func writeCertPair(certPath, keyPath string, certPEM, keyPEM []byte) error {
	_ = copyFile(certPath, certPath+".prev", 0o644)
	_ = copyFile(keyPath, keyPath+".prev", 0o600)

	keyTmp, err := writeTemp(keyPath, keyPEM, 0o600)
	if err != nil {
		return err
	}
	certTmp, err := writeTemp(certPath, certPEM, 0o644)
	if err != nil {
		os.Remove(keyTmp)
		return err
	}
	if err := os.Rename(keyTmp, keyPath); err != nil {
		os.Remove(keyTmp)
		os.Remove(certTmp)
		return err
	}
	if err := os.Rename(certTmp, certPath); err != nil {
		os.Remove(certTmp)
		_ = copyFile(keyPath+".prev", keyPath, 0o600)
		return err
	}
	return nil
}

func writeTemp(target string, data []byte, mode os.FileMode) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".tmp-*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(name)
		return "", err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		os.Remove(name)
		return "", err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(name)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, mode)
}

// InspectCertFile parses the first certificate in a file on the node.
func (s *Server) InspectCertFile(_ context.Context, req *pb.InspectCertFileRequest) (*pb.InspectCertFileResponse, error) {
	path := req.GetPath()
	if !filepath.IsAbs(path) {
		return nil, status.Error(codes.InvalidArgument, "path must be absolute")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "open %s: %v", path, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxInspectBytes))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "read %s: %v", path, err)
	}
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			return nil, status.Errorf(codes.InvalidArgument, "no certificate found in %s", path)
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		leaf, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "parse certificate in %s: %v", path, err)
		}
		return &pb.InspectCertFileResponse{
			Subject:       leaf.Subject.CommonName,
			DnsNames:      leaf.DNSNames,
			Issuer:        leaf.Issuer.CommonName,
			NotBeforeUnix: leaf.NotBefore.Unix(),
			NotAfterUnix:  leaf.NotAfter.Unix(),
			Serial:        leaf.SerialNumber.String(),
		}, nil
	}
}
