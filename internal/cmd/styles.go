package cmd

import "github.com/charmbracelet/lipgloss"

var (
	HeaderStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170"))
	DimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	SuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	WarnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
	ErrorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
)
