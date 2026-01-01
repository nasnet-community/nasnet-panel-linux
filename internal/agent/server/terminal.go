package server

import (
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/creack/pty"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	log "github.com/sirupsen/logrus"
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
	defer func() {
		_ = ptmx.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		log.Info("Terminal: PTY session closed")
	}()

	log.Infof("Terminal: PTY started with shell %s (PID: %d)", shell, cmd.Process.Pid)

	// Use context for graceful shutdown
	ctx := stream.Context()

	// Error channel for goroutine errors
	errChan := make(chan error, 2)
	var wg sync.WaitGroup
	var sendMu sync.Mutex

	// Goroutine: Read PTY output → send to stream
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				n, err := ptmx.Read(buf)
				if err != nil {
					if err != io.EOF {
						log.Debugf("Terminal: PTY read error: %v", err)
					}
					errChan <- err
					return
				}
				if n > 0 {
					if err := sendTerminal(stream, &sendMu, &pb.TerminalOutput{
						Payload: &pb.TerminalOutput_Data{Data: buf[:n]},
					}); err != nil {
						log.Debugf("Terminal: Stream send error: %v", err)
						errChan <- err
						return
					}
				}
			}
		}
	}()

	// Main loop: Receive from stream → write to PTY
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				in, err := stream.Recv()
				if err != nil {
					if err != io.EOF {
						log.Debugf("Terminal: Stream recv error: %v", err)
					}
					errChan <- err
					return
				}

				switch p := in.Payload.(type) {
				case *pb.TerminalInput_Data:
					if _, err := ptmx.Write(p.Data); err != nil {
						log.Debugf("Terminal: PTY write error: %v", err)
						errChan <- err
						return
					}

				case *pb.TerminalInput_Resize:
					if p.Resize != nil {
						if err := pty.Setsize(ptmx, &pty.Winsize{
							Rows: uint16(p.Resize.Rows),
							Cols: uint16(p.Resize.Cols),
						}); err != nil {
							log.Warnf("Terminal: Failed to resize PTY: %v", err)
						} else {
							log.Debugf("Terminal: Resized to %dx%d", p.Resize.Cols, p.Resize.Rows)
						}
					}

				case *pb.TerminalInput_Close:
					log.Info("Terminal: Close signal received")
					return
				}
			}
		}
	}()

	// Wait for process to complete or error
	exitCode := 0
	done := make(chan struct{})
	go func() {
		err := cmd.Wait()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
		}
		close(done)
	}()

	// Wait for either context cancellation, error, or process exit
	select {
	case <-ctx.Done():
		log.Info("Terminal: Context cancelled")
	case err := <-errChan:
		if err != io.EOF {
			log.Debugf("Terminal: Error in goroutine: %v", err)
		}
	case <-done:
		log.Infof("Terminal: Shell exited with code %d", exitCode)
		_ = sendTerminal(stream, &sendMu, &pb.TerminalOutput{
			Payload: &pb.TerminalOutput_ExitCode{ExitCode: int32(exitCode)},
		})
	}

	return nil
}
