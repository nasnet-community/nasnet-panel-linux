package ui

import (
	"github.com/charmbracelet/huh"
)

// Menu displays a select menu with the given title and options.
// Appends a "← Back" option automatically.
// Returns the selected index (0-based), or -1 if Back/Escape was chosen.
func Menu(title string, options []string) (int, error) {
	opts := make([]huh.Option[int], len(options)+1)
	for i, o := range options {
		opts[i] = huh.NewOption(o, i)
	}
	opts[len(options)] = huh.NewOption("← Back", -1)

	var choice int
	err := huh.NewSelect[int]().
		Title(title).
		Options(opts...).
		Value(&choice).
		WithTheme(Theme()).
		Run()

	if err != nil {
		return -1, err
	}
	return choice, nil
}
