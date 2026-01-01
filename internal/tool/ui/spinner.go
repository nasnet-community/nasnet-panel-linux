package ui

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func Spinner(message string, cmd *exec.Cmd) error {
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Start(); err != nil {
		fmt.Printf("  %s %s\n", SymbolCross, message)
		return err
	}

	errCh := make(chan error, 1)
	go func() { errCh <- cmd.Wait() }()

	fmt.Print("\033[?25l")
	defer fmt.Print("\033[?25h")

	i := 0
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case err := <-errCh:
			if err != nil {
				fmt.Printf("\r  %s %s\n", SymbolCross, message)
				if buf.Len() > 0 {
					lines := strings.SplitN(buf.String(), "\n", 6)
					if len(lines) > 5 {
						lines = lines[:5]
					}
					for _, l := range lines {
						if l != "" {
							fmt.Printf("    %s\n", StyleDim.Render(l))
						}
					}
				}
				return err
			}
			fmt.Printf("\r  %s %s\n", SymbolCheck, message)
			return nil
		case <-ticker.C:
			fmt.Printf("\r  %s %s", StyleCyan.Render(spinnerFrames[i%len(spinnerFrames)]), message)
			i++
		}
	}
}
