package tui

import lipgloss "charm.land/lipgloss/v2"

var (
	titleStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	cardStyle         = lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("8")).Padding(0, 1)
	selectedCardStyle = cardStyle.BorderForeground(lipgloss.Color("12"))
	runningStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	stoppedStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	failedStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
	confirmStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
	errorStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)
