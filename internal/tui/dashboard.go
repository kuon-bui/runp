package tui

import (
	"fmt"
	"strings"

	lipgloss "charm.land/lipgloss/v2"

	"runp/internal/controller"
	"runp/internal/process"
)

func renderDashboard(snapshot controller.Snapshot, projectIndex, processIndex, width int) string {
	var content strings.Builder
	content.WriteString(titleStyle.Render("runp"))
	content.WriteString("\n\n")
	if len(snapshot.Projects) == 0 {
		content.WriteString("No projects. Press a to add project.\n")
		return content.String()
	}

	cardWidth := width - 4
	columns := 1
	if width >= 90 {
		columns = 2
		cardWidth = (width - 6) / 2
	}
	cards := make([]string, 0, len(snapshot.Projects))
	for index, project := range snapshot.Projects {
		var body strings.Builder
		body.WriteString(project.Name)
		body.WriteByte('\n')
		for processIndexInProject, item := range project.Processes {
			marker := "  "
			if index == projectIndex && processIndexInProject == processIndex {
				marker = "› "
			}
			body.WriteString(marker)
			body.WriteString(item.Name)
			body.WriteString("  ")
			body.WriteString(renderState(item.Runtime.State))
			body.WriteByte('\n')
		}
		style := cardStyle
		if index == projectIndex {
			style = selectedCardStyle
		}
		cards = append(cards, style.Width(max(1, cardWidth-4)).Render(strings.TrimSuffix(body.String(), "\n")))
	}
	for index := 0; index < len(cards); index += columns {
		if columns == 2 && index+1 < len(cards) {
			content.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, cards[index], "  ", cards[index+1]))
		} else {
			content.WriteString(cards[index])
		}
		content.WriteByte('\n')
	}
	return content.String()
}

func renderState(state process.State) string {
	label := strings.ToUpper(string(state))
	if label == "" {
		label = "STOPPED"
	}
	switch state {
	case process.Running:
		return runningStyle.Render(label)
	case process.Failed, process.Blocked:
		return failedStyle.Render(label)
	default:
		return stoppedStyle.Render(label)
	}
}

func footer(width int) string {
	if width < 70 {
		return "↑↓ project  ←→ process  s start  k stop  q quit"
	}
	return fmt.Sprintf("%-14s %-14s %-14s %-14s", "↑/↓ project", "←/→ process", "s start", "k stop") + "r restart  q quit"
}
