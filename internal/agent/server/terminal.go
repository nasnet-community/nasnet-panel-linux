package server

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

// sendTerminal serializes Send calls on a bidi terminal stream.
// gRPC streams are not safe for concurrent Send.
func sendTerminal(stream interface {
	Send(*pb.TerminalOutput) error
}, mu *sync.Mutex, out *pb.TerminalOutput) error {
	mu.Lock()
	defer mu.Unlock()
	return stream.Send(out)
}

// OpenTerminal starts an interactive PTY shell session using bidirectional streaming
func (s *Server) OpenTerminal(stream pb.NodeAgent_OpenTerminalServer) error {
	log.Info("Terminal: New PTY session requested")

	// Determine shell to use
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
		if _, err := os.Stat(shell); os.IsNotExist(err) {
			shell = "/bin/sh"
		}
	}

	// Start PTY with interactive shell for readline support (tab completion, arrow keys)
	cmd := exec.Command(shell, "-i")

	// Build environment: filter out existing TERM to avoid conflicts, then set xterm-256color
	env := os.Environ()
	filteredEnv := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, "TERM=") {
			filteredEnv = append(filteredEnv, e)
		}
	}
	cmd.Env = append(filteredEnv, "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		log.Errorf("Terminal: Failed to start PTY: %v", err)
		return stream.Send(&pb.TerminalOutput{
			Payload: &pb.TerminalOutput_Error{Error: "failed to start PTY: " + err.Error()},
		})
	}
	// pty.Start uses Fd(), which disables Go's poller. Reopen a nonblocking
	// duplicate so Close interrupts reads, including after terminal resizes.
	fd, err := unix.FcntlInt(ptmx.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err == nil {
		err = syscall.SetNonblock(fd, true)
		if err != nil {
			_ = syscall.Close(fd)
		}
	}
	_ = ptmx.Close()
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	ptmx = os.NewFile(uintptr(fd), "terminal-pty")
	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()

	// Only one goroutine calls Wait. PTY EOF can precede process reaping;
	// it must not win a race that discards the shell's actual exit status.
	processDone := make(chan struct{})
	exitCode := int32(0)
	go func() {
		if err := cmd.Wait(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = int32(exitErr.ExitCode())
			}
		}
		close(processDone)
	}()

	done := make(chan struct{})
	var finishOnce sync.Once
	finish := func() { finishOnce.Do(func() { close(done) }) }
	outputDone := make(chan struct{})
	var sendMu sync.Mutex
	defer func() {
		cancel()
		_ = cmd.Process.Kill()
		_ = ptmx.Close()
		<-processDone
		// Returning this server RPC cancels any blocked transport Send/Recv.
		// Waiting for those calls here would prevent gRPC from releasing them.
		log.Info("Terminal: PTY session closed")
	}()

	// An empty data frame acknowledges successful PTY startup without waiting
	// for a prompt. Silent shells can then accept their first input.
	if err := sendTerminal(stream, &sendMu, &pb.TerminalOutput{
		Payload: &pb.TerminalOutput_Data{Data: []byte{}},
	}); err != nil {
		return err
	}

	go func() {
		defer close(outputDone)
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				if err := sendTerminal(stream, &sendMu, &pb.TerminalOutput{
					Payload: &pb.TerminalOutput_Data{Data: append([]byte(nil), buf[:n]...)},
				}); err != nil {
					finish()
					return
				}
			}
			if err != nil {
				// Linux PTYs report EIO when the shell closes its slave fd.
				if !errors.Is(err, io.EOF) && !errors.Is(err, syscall.EIO) && !errors.Is(err, os.ErrClosed) && ctx.Err() == nil {
					log.Debugf("Terminal: PTY read error: %v", err)
					finish()
				}
				return
			}
		}
	}()

	// A server stream's Recv is released when this RPC returns. It cannot be
	// joined before returning on shell exit; cancellation prevents late input
	// from being applied while the transport finishes closing.
	go func() {
		defer finish()
		for {
			in, err := stream.Recv()
			if err != nil || ctx.Err() != nil {
				return
			}
			switch p := in.Payload.(type) {
			case *pb.TerminalInput_Data:
				if _, err := ptmx.Write(p.Data); err != nil {
					return
				}
			case *pb.TerminalInput_Resize:
				if p.Resize != nil {
					if err := pty.Setsize(ptmx, &pty.Winsize{
						Rows: uint16(p.Resize.Rows),
						Cols: uint16(p.Resize.Cols),
					}); err != nil {
						log.Warnf("Terminal: Failed to resize PTY: %v", err)
					}
				}
			case *pb.TerminalInput_Close:
				return
			}
		}
	}()

	select {
	case <-ctx.Done():
	case <-done:
	case <-processDone:
		// Drain final output, then detach children that still hold the PTY open.
		drain := time.AfterFunc(250*time.Millisecond, func() { _ = ptmx.Close() })
		defer drain.Stop()
		select {
		case <-outputDone:
		case <-ctx.Done():
			return nil
		case <-done:
			return nil
		}
		_ = sendTerminal(stream, &sendMu, &pb.TerminalOutput{
			Payload: &pb.TerminalOutput_ExitCode{ExitCode: exitCode},
		})
	}
	return nil
}
