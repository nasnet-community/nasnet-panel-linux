package realitydest

import (
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// ToProto shapes a request for the agent RPC.
func ToProto(r Request) *pb.RealityDestProbeRequest {
	return &pb.RealityDestProbeRequest{
		Dest:        r.Dest,
		Type:        r.Type,
		Xver:        r.Xver,
		ServerNames: r.ServerNames,
		TimeoutMs:   int32(r.Timeout / time.Millisecond),
	}
}

// FromProtoRequest is the agent-side inverse of ToProto.
func FromProtoRequest(p *pb.RealityDestProbeRequest) Request {
	return Request{
		Dest:        p.GetDest(),
		Type:        p.GetType(),
		Xver:        p.GetXver(),
		ServerNames: p.GetServerNames(),
		Timeout:     time.Duration(p.GetTimeoutMs()) * time.Millisecond,
	}
}

// ReportToProto serialises a report for the wire.
func ReportToProto(r *Report) *pb.RealityDestProbeResponse {
	out := &pb.RealityDestProbeResponse{Ok: r.OK, Dest: r.Dest, Summary: r.Summary, Warnings: r.Warnings}
	for _, c := range r.Checks {
		pc := &pb.RealityDestCheck{
			ServerName:         c.ServerName,
			Ok:                 c.OK,
			Reason:             c.Reason,
			Message:            c.Message,
			TlsVersion:         c.TLSVersion,
			KeyShare:           c.KeyShare,
			CipherSuite:        c.CipherSuite,
			Alpn:               c.ALPN,
			HandshakeMs:        c.HandshakeMs,
			FlightBytes:        int32(c.FlightBytes),
			LargestRecordBytes: int32(c.LargestRecordBytes),
			CertSubject:        c.CertSubject,
			CertIssuer:         c.CertIssuer,
			CertError:          c.CertError,
			Warnings:           c.Warnings,
		}
		if c.CertNotAfter != nil {
			pc.CertNotAfter = c.CertNotAfter.Unix()
		}
		out.Checks = append(out.Checks, pc)
	}
	return out
}

// ReportFromProto rebuilds a report received from an agent.
func ReportFromProto(p *pb.RealityDestProbeResponse) *Report {
	r := &Report{OK: p.GetOk(), Dest: p.GetDest(), Summary: p.GetSummary(), Warnings: p.GetWarnings(), CheckedAt: time.Now(), Checks: make([]Check, 0, len(p.GetChecks()))}
	for _, pc := range p.GetChecks() {
		c := Check{
			ServerName:         pc.GetServerName(),
			OK:                 pc.GetOk(),
			Reason:             pc.GetReason(),
			Message:            pc.GetMessage(),
			TLSVersion:         pc.GetTlsVersion(),
			KeyShare:           pc.GetKeyShare(),
			CipherSuite:        pc.GetCipherSuite(),
			ALPN:               pc.GetAlpn(),
			HandshakeMs:        pc.GetHandshakeMs(),
			FlightBytes:        int(pc.GetFlightBytes()),
			LargestRecordBytes: int(pc.GetLargestRecordBytes()),
			CertSubject:        pc.GetCertSubject(),
			CertIssuer:         pc.GetCertIssuer(),
			CertError:          pc.GetCertError(),
			Warnings:           pc.GetWarnings(),
		}
		if pc.GetCertNotAfter() != 0 {
			t := time.Unix(pc.GetCertNotAfter(), 0).UTC()
			c.CertNotAfter = &t
		}
		r.Checks = append(r.Checks, c)
	}
	return r
}
