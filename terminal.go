package main

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

func terminalText(value string) string {
	return strings.Map(func(char rune) rune {
		if unicode.IsControl(char) && char != '\n' && char != '\t' {
			return -1
		}
		return char
	}, ansi.Strip(value))
}
