package ui

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

var (
	StyleTitle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	StyleCyan    = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	StyleSuccess = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	StyleError   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	StyleWarning = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	StyleDim     = lipgloss.NewStyle().Faint(true)

	SymbolCheck = StyleSuccess.Render("✔")
	SymbolCross = StyleError.Render("✘")
	SymbolArrow = StyleCyan.Render("▸")
	SymbolWarn  = StyleWarning.Render("⚠")
)

func Theme() *huh.Theme {
	t := huh.ThemeBase()

	t.Focused.Title = t.Focused.Title.Foreground(lipgloss.Color("6")).Bold(true)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(lipgloss.Color("6"))
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(lipgloss.Color("6"))
	t.Focused.FocusedButton = t.Focused.FocusedButton.Background(lipgloss.Color("6")).Foreground(lipgloss.Color("0"))
	t.Focused.BlurredButton = t.Focused.BlurredButton.Foreground(lipgloss.Color("8"))

	return t
}

func StepOk(msg string) {
	fmt.Printf("  %s %s\n", SymbolCheck, msg)
}

func StepFail(msg string) {
	fmt.Printf("  %s %s\n", SymbolCross, msg)
}

func StepWarn(msg string) {
	fmt.Printf("  %s %s\n", SymbolWarn, msg)
}

func StepInfo(msg string) {
	fmt.Printf("  %s %s\n", SymbolArrow, msg)
}
