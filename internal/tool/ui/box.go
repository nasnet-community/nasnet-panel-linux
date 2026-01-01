package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var boxStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.DoubleBorder()).
	BorderForeground(lipgloss.Color("6")).
	Padding(0, 2).
	Width(50).
	Align(lipgloss.Center)

func DrawBox(title string) {
	fmt.Println()
	fmt.Println(boxStyle.Render(StyleTitle.Render(title)))
	fmt.Println()
}

func DrawHeader(title string) {
	fmt.Println()
	fmt.Printf("  %s\n", StyleCyan.Bold(true).Render("── "+title+" ──"))
	fmt.Println()
}

func DrawSeparator() {
	fmt.Printf("  %s\n", StyleDim.Render(strings.Repeat("─", 50)))
}

func PressAnyKey() {
	fmt.Printf("\n  %s", StyleDim.Render("Press Enter to continue..."))
	buf := make([]byte, 1)
	os.Stdin.Read(buf)
	fmt.Println()
}

func ClearScreen() {
	fmt.Print("\033[H\033[2J")
}
