package tui

import lipgloss "charm.land/lipgloss/v2"

const (
	colorAccent  = "14"
	colorMuted   = "8"
	colorSuccess = "10"
	colorError   = "9"
	colorWarning = "11"
	colorSurface = "236"

	defaultTerminalWidth  = 80
	defaultTerminalHeight = 24
	wideDashboardWidth    = 100
	mediumDashboardWidth  = 70
	projectPaneMinWidth   = 16
	projectPaneMaxWidth   = 24
	processPaneMinWidth   = 30
	processPaneMaxWidth   = 46
	rootChromeHeight      = 2
	paneFrameWidth        = 2
	paneHorizontalPadding = 1

	wideFormBreakpoint      = 80
	compactFooterBreakpoint = 70
	cardHorizontalPadding   = 1
	panelGap                = 2
	formSidebarWidth        = 16
)

var (
	appHeaderStyle = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color(colorAccent))
	appFooterStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorMuted))
	paneStyle = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(colorMuted)).Padding(0, paneHorizontalPadding)
	paneTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorMuted))
	selectionStyle = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color(colorAccent))
	runningStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorSuccess))
	transitionalStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorWarning))
	stoppedStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted))
	failedStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorError))
	errorStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color(colorError))
	overlayStyle      = lipgloss.NewStyle().
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(lipgloss.Color(colorMuted)).
				Padding(1, 2)
	overlayTitleStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorAccent))
	shortcutSectionStyle  = lipgloss.NewStyle().Width(44).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(colorMuted)).
			Padding(0, 1)
	confirmTitleStyle      = overlayTitleStyle.Foreground(lipgloss.Color(colorWarning))
	formHeaderStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorAccent))
	formMutedStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted))
	formSectionStyle       = lipgloss.NewStyle().Padding(0, cardHorizontalPadding)
	formActiveSectionStyle = formSectionStyle.Bold(true).
				Foreground(lipgloss.Color(colorAccent))
	formInputStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(lipgloss.Color(colorMuted)).Padding(0, 1)
	formFocusedInputStyle = lipgloss.NewStyle().
				Border(lipgloss.ThickBorder(), false, false, true, false).
				BorderForeground(lipgloss.Color(colorAccent)).Padding(0, 1)
	formInlineErrorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(colorError))
	formSwitchStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted))
	formEnabledSwitchStyle = formSwitchStyle.Foreground(lipgloss.Color(colorSuccess)).Bold(true)
	formErrorStyle         = lipgloss.NewStyle().
				BorderStyle(lipgloss.NormalBorder()).
				BorderBottom(true).
				BorderForeground(lipgloss.Color(colorError)).
				Foreground(lipgloss.Color(colorError)).
				Padding(0, cardHorizontalPadding)
	formModalStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(colorMuted))
)
