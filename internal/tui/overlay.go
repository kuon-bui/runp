package tui

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"
)

func composeOverlay(background, foreground string, width, height int) string {
	return composeOverlayIn(background, foreground, width, height, 0, 0, width, height)
}

func composeOverlayIn(background, foreground string, width, height, x, y, areaWidth, areaHeight int) string {
	width, height = max(width, 1), max(height, 1)
	background = fitScreen(background, width, height)
	areaWidth, areaHeight = max(areaWidth, 1), max(areaHeight, 1)
	foregroundWidth := min(lipgloss.Width(foreground), areaWidth)
	foregroundHeight := min(lipgloss.Height(foreground), areaHeight)
	x += max((areaWidth-foregroundWidth)/2, 0)
	y += max((areaHeight-foregroundHeight)/2, 0)
	foreground = lipgloss.NewStyle().MaxWidth(areaWidth).MaxHeight(areaHeight).Render(foreground)
	canvas := lipgloss.NewCanvas(width, height)
	canvas.Compose(lipgloss.NewCompositor(
		lipgloss.NewLayer(background).Z(0),
		lipgloss.NewLayer(foreground).X(x).Y(y).Z(1),
	))
	return fitScreen(canvas.Render(), width, height)
}

func renderProjectMenu() string {
	return overlayStyle.Render(overlayTitleStyle.Render("PROJECT ACTIONS") +
		"\n\n[s] Start  [k] Stop  [r] Restart  [e] Edit  [d] Delete\n[Esc] Cancel")
}

func renderAddMenu(selected int) string {
	items := []string{"[p] Project", "[o] Process", "[Esc] Cancel"}
	for index := range items {
		prefix := "  "
		if index == selected {
			prefix = "› "
			items[index] = selectionStyle.Render(prefix + items[index])
			continue
		}
		items[index] = prefix + items[index]
	}
	return overlayStyle.Render(overlayTitleStyle.Render("ADD") + "\n\n" + strings.Join(items, "\n\n"))
}

func renderShortcuts() string {
	dashboard := renderShortcutSection("DASHBOARD", `[↑↓/←→] Move
[Enter] Open logs
[s/k/r] Start / Stop / Restart
[g/a/e/d] Project menu / Add / Edit / Delete process
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
[e/d] Edit / Delete project
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
	case clearLog:
		name = "CLEAR LOG"
	case saveCritical:
		name = "SAVE AND RESTART"
	case deleteProcess:
		name = "DELETE PROCESS"
	case deleteProject:
		name = "DELETE PROJECT"
	}
	return overlayStyle.Render(confirmTitleStyle.Render("CONFIRM "+name) +
		"\n\n[y] Yes  [n/Esc] No")
}

func renderBusy() string {
	return overlayStyle.Render(overlayTitleStyle.Render("WORKING") +
		"\n\nStopping processes…")
}

func renderOperationError(err error) string {
	return overlayStyle.Render(errorStyle.Render("ERROR") + "\n\n" + err.Error() + "\n\n[Enter/Esc] Close")
}
