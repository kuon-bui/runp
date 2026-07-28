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
	formFrameStyle    = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("8")).
				Padding(0, 1)
	formHeaderStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	formMutedStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	formSectionStyle       = lipgloss.NewStyle().Padding(0, 1)
	formActiveSectionStyle = formSectionStyle.Bold(true).
				Foreground(lipgloss.Color("12")).
				Background(lipgloss.Color("236"))
	formInputStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("8")).
			Padding(0, 1)
	formFocusedInputStyle = formInputStyle.BorderForeground(lipgloss.Color("12"))
	formToggleStyle       = lipgloss.NewStyle().
				Foreground(lipgloss.Color("8")).
				Padding(0, 1)
	formEnabledToggleStyle = formToggleStyle.Foreground(lipgloss.Color("10")).Bold(true)
	formErrorStyle         = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("9")).
				Foreground(lipgloss.Color("9")).
				Padding(0, 1)
)
