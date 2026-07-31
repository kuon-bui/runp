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

func renderShortcuts() string {
	dashboard := renderShortcutSection("DASHBOARD", `[↑↓/←→] Move
[Enter] Open logs
[s/k/r] Start / Stop / Restart
[g/a/e] Project menu / Add / Edit
[?] Open or close shortcuts
[q/Ctrl+C] Quit`)
	logs := renderShortcutSection("LOGS & SEARCH", `[Esc] Back
[f/t] Follow / Stream
[/] Search
[n/N] Next / Previous match
[↑↓/PgUp/PgDn] Scroll
[Enter/Esc] Apply / Cancel search`)
	form := renderShortcutSection("FORM", `[Ctrl+S/Esc] Save / Cancel
[Tab/Shift+Tab/↑↓] Move field
[←→] Change choice
[Space] Toggle
[Enter] Set environment
[Ctrl+X] Delete environment`)
	menus := renderShortcutSection("MENUS & CONFIRM", `[s/k/r] Start / Stop / Restart project
[e] Edit project
[p] Add project
[o] Add process
[Esc/g/a] Close menu
[y/n/Esc] Confirm / Cancel`)
	sections := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.JoinVertical(lipgloss.Left, dashboard, logs),
		"  ",
		lipgloss.JoinVertical(lipgloss.Left, form, menus),
	)
	return overlayStyle.Render(overlayTitleStyle.Render("KEYBOARD SHORTCUTS") + "\n" + sections)
}

func renderShortcutSection(title, shortcuts string) string {
	return shortcutSectionStyle.Render(paneTitleStyle.Render(title) + "\n" + shortcuts)
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
