package ui

import (
	"fmt"

	"github.com/charmbracelet/huh"
)

func Confirm(message string) (bool, error) {
	var confirmed bool
	err := huh.NewConfirm().
		Title(message).
		Affirmative("Yes").
		Negative("No").
		Value(&confirmed).
		WithTheme(Theme()).
		Run()
	return confirmed, err
}

func ConfirmDangerous(message, word string) (bool, error) {
	fmt.Printf("\n  %s %s\n\n", StyleError.Bold(true).Render("DANGER"), StyleError.Render(message))

	var input string
	err := huh.NewInput().
		Title(fmt.Sprintf("Type %s to confirm", StyleTitle.Render(word))).
		Value(&input).
		WithTheme(Theme()).
		Run()
	if err != nil {
		return false, err
	}
	return input == word, nil
}
