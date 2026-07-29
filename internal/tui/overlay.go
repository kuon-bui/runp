package tui

import lipgloss "charm.land/lipgloss/v2"

func composeOverlay(background, foreground string, width, height int) string {
	width, height = max(width, 1), max(height, 1)
	background = fitScreen(background, width, height)
	foreground = lipgloss.NewStyle().MaxWidth(width).MaxHeight(height).Render(foreground)
	x := max((width-lipgloss.Width(foreground))/2, 0)
	y := max((height-lipgloss.Height(foreground))/2, 0)
	canvas := lipgloss.NewCanvas(width, height)
	canvas.Compose(lipgloss.NewCompositor(
		lipgloss.NewLayer(background).Z(0),
		lipgloss.NewLayer(foreground).X(x).Y(y).Z(1),
	))
	return fitScreen(canvas.Render(), width, height)
}

func renderProjectMenu() string {
	return overlayStyle.Render(overlayTitleStyle.Render("PROJECT ACTIONS") +
		"\n\n[s] Start  [k] Stop  [r] Restart  [e] Edit\n[Esc] Cancel")
}

func renderAddMenu() string {
	return overlayStyle.Render(overlayTitleStyle.Render("ADD") +
		"\n\n[p] Project  [o] Process\n[Esc] Cancel")
}

func renderConfirmation(current action) string {
	name := "ACTION"
	switch current {
	case stopProcess, stopProject:
		name = "STOP"
	case restartProcess, restartProject:
		name = "RESTART"
	case shutdown:
		name = "SHUTDOWN"
	case saveCritical:
		name = "SAVE AND RESTART"
	}
	return overlayStyle.Render(confirmTitleStyle.Render("CONFIRM "+name) +
		"\n\n[y] Yes  [n/Esc] No")
}

func renderBusy() string {
	return overlayStyle.Render(overlayTitleStyle.Render("WORKING") +
		"\n\nStopping processes…")
}

func renderOperationError(err error) string {
	return overlayStyle.Render(errorStyle.Render("ERROR") + "\n\n" + err.Error())
}
