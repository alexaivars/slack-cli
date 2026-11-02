package main

import (
	"errors"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"
)

type confirmModel struct {
	prompt   string
	accepted bool
}

func confirmAction(prompt string) (bool, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return false, errors.New("confirmation requires a terminal; pass --yes for non-interactive use")
	}
	model, err := tea.NewProgram(confirmModel{prompt: prompt}).Run()
	if err != nil {
		return false, err
	}
	result, ok := model.(confirmModel)
	if !ok {
		return false, nil
	}
	return result.accepted, nil
}

func (m confirmModel) Init() tea.Cmd {
	return nil
}

func (m confirmModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.KeyPressMsg:
		switch strings.ToLower(message.String()) {
		case "y":
			m.accepted = true
			return m, tea.Quit
		case "n", "esc", "ctrl+c", "q":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m confirmModel) View() tea.View {
	return tea.NewView(m.prompt + " [y/N] ")
}
