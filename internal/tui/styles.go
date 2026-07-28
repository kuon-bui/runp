package tui

import lipgloss "charm.land/lipgloss/v2"

const (
	colorAccent  = "12"
	colorMuted   = "8"
	colorSuccess = "10"
	colorError   = "9"
	colorWarning = "11"
	colorSurface = "236"

	defaultTerminalWidth     = 80
	defaultTerminalHeight    = 24
	wideFormBreakpoint       = 80
	wideDashboardBreakpoint  = 90
	compactFooterBreakpoint  = 70
	cardHorizontalPadding    = 1
	dashboardHorizontalInset = 4
	dashboardTwoColumnInset  = 6
	cardFrameWidth           = 4
	panelGap                 = 2
	formOuterInset           = 4
	formInnerInset           = 4
	formSidebarWidth         = 16
	minimumPanelWidth        = 24
)

var (
	titleStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorAccent))
	cardStyle         = lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(colorMuted)).Padding(0, cardHorizontalPadding)
	selectedCardStyle = cardStyle.BorderForeground(lipgloss.Color(colorAccent))
	runningStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorSuccess))
	stoppedStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted))
	failedStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorError))
	confirmStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorWarning))
	errorStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color(colorError))
	formFrameStyle    = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color(colorMuted)).
				Padding(0, cardHorizontalPadding)
	formHeaderStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorAccent))
	formMutedStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted))
	formSectionStyle       = lipgloss.NewStyle().Padding(0, cardHorizontalPadding)
	formActiveSectionStyle = formSectionStyle.Bold(true).
				Foreground(lipgloss.Color(colorAccent)).
				Background(lipgloss.Color(colorSurface))
	formInputStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(colorMuted)).
			Padding(0, cardHorizontalPadding)
	formFocusedInputStyle = formInputStyle.BorderForeground(lipgloss.Color(colorAccent))
	formToggleStyle       = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colorMuted)).
				Padding(0, cardHorizontalPadding)
	formEnabledToggleStyle = formToggleStyle.Foreground(lipgloss.Color(colorSuccess)).Bold(true)
	formErrorStyle         = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color(colorError)).
				Foreground(lipgloss.Color(colorError)).
				Padding(0, cardHorizontalPadding)
)
