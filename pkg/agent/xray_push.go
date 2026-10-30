package agent

import (
	"context"
	"fmt"
	"io"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// XrayPushChunkSize is the most data one message of PushXrayBinary carries.
// The agent refuses more.
const XrayPushChunkSize = 1 << 20

// IsUnimplemented reports whether err is an agent saying it has no such call,
// which means it predates it and has to be updated first.
func IsUnimplemented(err error) bool {
	return status.Code(err) == codes.Unimplemented
}

// PushXrayBinary streams an xray build to the agent: a header, then r in
// chunks of at most XrayPushChunkSize. sum is the hex SHA256 of the unpacked
// binary; with gzipped set, r is that binary as one gzip stream. The agent
// checks it all before it touches xray.
//
// A refusal comes back as the agent's status error with its code intact, so
// IsUnimplemented can tell an agent that predates the call from one that said
// no. A push the agent took but couldn't finish (it rolled back) is a plain
// error with the agent's message. A call the context cut short returns the
// context's error; ctx is checked on every read of r, so only a Read that
// blocks outright can hold the call up.
func (c *Client) PushXrayBinary(ctx context.Context, version, sum string, r io.Reader, restartAfter, gzipped bool) error {
	// The unary interceptor skips streams, so the budget is applied here.
	ctx, stopTimer := WithRPCDeadline(ctx, "PushXrayBinary")
	defer stopTimer()
	// Whatever way this returns, the stream goes with it.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stream, err := c.client.PushXrayBinary(ctx)
	if err != nil {
		return pushError(ctx, err)
	}
	send := func(m *pb.XrayBinaryChunk) error {
		err := stream.Send(m)
		if err == io.EOF {
			// The agent ended the call early; its status says why.
			if _, err = stream.CloseAndRecv(); err == nil {
				err = io.ErrUnexpectedEOF
			}
		}
		return err
	}

	err = send(&pb.XrayBinaryChunk{Payload: &pb.XrayBinaryChunk_Header{Header: &pb.XrayBinaryHeader{
		Version:      version,
		Sha256:       sum,
		RestartAfter: restartAfter,
		Gzip:         gzipped,
	}}})
	if err != nil {
		return pushError(ctx, err)
	}

	src := &ctxReader{ctx: ctx, r: r}
	for {
		// A fresh buffer each time: gRPC may still look at a sent message.
		buf := make([]byte, XrayPushChunkSize)
		n, rerr := io.ReadFull(src, buf)
		if rerr != nil && rerr != io.EOF && rerr != io.ErrUnexpectedEOF {
			if ctx.Err() != nil {
				return pushError(ctx, rerr)
			}
			return fmt.Errorf("push xray binary: read the build: %w", rerr)
		}
		if n > 0 {
			if err := send(&pb.XrayBinaryChunk{Payload: &pb.XrayBinaryChunk_Data{Data: buf[:n]}}); err != nil {
				return pushError(ctx, err)
			}
		}
		if rerr != nil {
			break // the end of the build: this was the last chunk
		}
	}

	resp, err := stream.CloseAndRecv()
	if err != nil {
		return pushError(ctx, err)
	}
	if !resp.Success {
		return fmt.Errorf("xray push failed: %s", resp.Message)
	}
	return nil
}

// pushError words a failed push. When the context ended the call, its error
// is the cause worth reporting; otherwise the status stays reachable through
// errors.As, so its code does too.
func pushError(ctx context.Context, err error) error {
	if cerr := ctx.Err(); cerr != nil {
		return fmt.Errorf("push xray binary: %w", cerr)
	}
	return fmt.Errorf("push xray binary: %w", err)
}

// ctxReader stops reading once its context is done.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
