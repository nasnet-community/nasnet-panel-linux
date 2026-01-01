package ui

import (
	"github.com/charmbracelet/huh"
)

func InputString(label, placeholder string) (string, error) {
	var val string
	err := huh.NewInput().
		Title(label).
		Placeholder(placeholder).
		Value(&val).
		WithTheme(Theme()).
		Run()
	return val, err
}

func InputStringDefault(label, defaultVal string) (string, error) {
	val := defaultVal
	err := huh.NewInput().
		Title(label).
		Value(&val).
		WithTheme(Theme()).
		Run()
	return val, err
}

func InputPassword(label string) (string, error) {
	var val string
	err := huh.NewInput().
		Title(label).
		EchoMode(huh.EchoModePassword).
		Value(&val).
		WithTheme(Theme()).
		Run()
	return val, err
}
