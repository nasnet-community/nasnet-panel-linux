package ui

import (
	"fmt"
	"os"
	"os/exec"
)

func RunLogged(message string, cmd *exec.Cmd) error {
	fmt.Println()
	fmt.Printf("  %s %s\n", SymbolArrow, StyleTitle.Render(message))
	DrawSeparator()

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()

	DrawSeparator()
	if err != nil {
		fmt.Printf("  %s %s\n", SymbolCross, message)
		return err
	}
	fmt.Printf("  %s %s\n", SymbolCheck, message)
	return nil
}
