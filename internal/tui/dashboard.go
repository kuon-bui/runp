package tui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/viewport"
	lipgloss "charm.land/lipgloss/v2"

	"runp/internal/controller"
	"runp/internal/process"
)

type dashboardMode uint8

const (
	dashboardNarrow dashboardMode = iota
	dashboardMedium
	dashboardWide
)

type dashboardGeometry struct {
	mode                               dashboardMode
	bodyHeight, processHeight          int
	projectWidth, processWidth         int
	logWidth, logHeight, logBodyHeight int
}

func dashboardLayout(width, height int) dashboardGeometry {
	width, height = max(width, 1), max(height, 1)
	bodyHeight := max(height-rootChromeHeight, 1)
	if width >= wideDashboardWidth {
		projects := min(max(width*20/100, projectPaneMinWidth), projectPaneMaxWidth)
		processes := min(max(width*38/100, processPaneMinWidth), processPaneMaxWidth)
		return dashboardGeometry{
			mode: dashboardWide, bodyHeight: bodyHeight, processHeight: bodyHeight,
			projectWidth: projects, processWidth: processes,
			logWidth: max(width-projects-processes, 1), logHeight: bodyHeight,
			logBodyHeight: max(bodyHeight-paneFrameWidth-1, 1),
		}
	}
	if width >= mediumDashboardWidth {
		left := min(max(width*42/100, processPaneMinWidth), processPaneMaxWidth)
		return dashboardGeometry{
			mode: dashboardMedium, bodyHeight: bodyHeight, processHeight: bodyHeight,
			processWidth: left, logWidth: max(width-left, 1), logHeight: bodyHeight,
			logBodyHeight: max(bodyHeight-paneFrameWidth-1, 1),
		}
	}
	processHeight := max(bodyHeight/3, 3)
	logHeight := max(bodyHeight-processHeight, 1)
	return dashboardGeometry{
		mode: dashboardNarrow, bodyHeight: bodyHeight, processHeight: processHeight,
		processWidth: width, logWidth: width, logHeight: logHeight,
		logBodyHeight: max(logHeight-paneFrameWidth-1, 1),
	}
}

func fitScreen(content string, width, height int) string {
	width, height = max(width, 1), max(height, 1)
	return lipgloss.NewStyle().Width(width).Height(height).
		MaxWidth(width).MaxHeight(height).
		Background(lipgloss.Color(colorSurface)).Render(content)
}

func renderVisibleLines(lines []string, selected, width, height int) string {
	view := viewport.New(
		viewport.WithWidth(max(width, 1)),
		viewport.WithHeight(max(height, 1)),
	)
	view.SetContentLines(lines)
	if selected >= 0 {
		view.SetYOffset(min(max(selected-height+1, 0), max(len(lines)-height, 0)))
	}
	return view.View()
}

func renderPane(title, body string, width, height int) string {
	innerWidth := max(width-paneFrameWidth-2*paneHorizontalPadding, 1)
	innerHeight := max(height-paneFrameWidth, 1)
	content := paneTitleStyle.MaxWidth(innerWidth).Render(title)
	if innerHeight > 1 {
		content += "\n" + lipgloss.NewStyle().MaxWidth(innerWidth).MaxHeight(innerHeight-1).Render(body)
	}
	return paneStyle.Width(width).Height(height).Render(content)
}

func renderPID(pid int) string {
	if pid <= 0 {
		return "—"
	}
	return strconv.Itoa(pid)
}

func processColumns(width int) string {
	nameWidth := max(width-20, 1)
	return lipgloss.NewStyle().MaxWidth(width).Render(
		fmt.Sprintf("  %-*s %10s %5s", nameWidth, "NAME", "STATE", "PID"),
	)
}

func renderState(state process.State) string {
	label := stateLabel(state)
	switch state {
	case process.Running:
		return runningStyle.Render(label)
	case process.Starting, process.Stopping, process.Restarting:
		return transitionalStyle.Render(label)
	case process.Failed, process.Blocked:
		return failedStyle.Render(label)
	default:
		return stoppedStyle.Render(label)
	}
}

func stateLabel(state process.State) string {
	if label := strings.ToUpper(string(state)); label != "" {
		return label
	}
	return "STOPPED"
}

func projectRows(snapshot controller.Snapshot, selected int) []string {
	rows := make([]string, 0, len(snapshot.Projects))
	for index, project := range snapshot.Projects {
		marker := "  "
		if index == selected {
			marker = "› "
		}
		row := marker + project.Name
		if index == selected {
			row = selectedRowStyle.Render(row)
		}
		rows = append(rows, row)
	}
	return rows
}

func processRow(item controller.ProcessSnapshot, selected bool, width int) string {
	nameWidth := max(width-20, 1)
	marker := "  "
	if selected {
		marker = "› "
	}
	name := lipgloss.NewStyle().Inline(true).MaxWidth(nameWidth).Render(item.Name)
	stateValue := renderState(item.Runtime.State)
	if selected {
		stateValue = stateLabel(item.Runtime.State)
	}
	state := lipgloss.NewStyle().Width(10).MaxWidth(10).Render(stateValue)
	row := fmt.Sprintf("%s%-*s %s %5s", marker, nameWidth, name, state, renderPID(item.Runtime.PID))
	if selected {
		row = selectedRowStyle.Width(width).MaxWidth(width).Render(row)
	}
	return row
}

func currentProcessRows(snapshot controller.Snapshot, projectIndex, processIndex, width int) []string {
	if projectIndex < 0 || projectIndex >= len(snapshot.Projects) {
		return nil
	}
	items := snapshot.Projects[projectIndex].Processes
	rows := make([]string, 0, len(items))
	for index, item := range items {
		rows = append(rows, processRow(item, index == processIndex, width))
	}
	return rows
}

func groupedProcessRows(snapshot controller.Snapshot, projectIndex, processIndex, width int) ([]string, int) {
	rows := make([]string, 0)
	selectedLine := -1
	for currentProject, project := range snapshot.Projects {
		title := paneTitleStyle.Render(strings.ToUpper(project.Name))
		if currentProject == projectIndex {
			title = selectedRowStyle.Render("› " + strings.ToUpper(project.Name))
		}
		rows = append(rows, title)
		for currentProcess, item := range project.Processes {
			selected := currentProject == projectIndex && currentProcess == processIndex
			if selected {
				selectedLine = len(rows)
			}
			rows = append(rows, processRow(item, selected, width))
		}
	}
	return rows, selectedLine
}

func processCounts(snapshot controller.Snapshot) (running, total int) {
	for _, project := range snapshot.Projects {
		for _, item := range project.Processes {
			total++
			if item.Runtime.State == process.Running {
				running++
			}
		}
	}
	return running, total
}

func renderAppHeader(snapshot controller.Snapshot, projectIndex, processIndex, width int) string {
	running, total := processCounts(snapshot)
	counts := fmt.Sprintf("%d PROJECTS  %d/%d RUNNING", len(snapshot.Projects), running, total)
	context := ""
	if projectIndex >= 0 && projectIndex < len(snapshot.Projects) {
		project := snapshot.Projects[projectIndex]
		context = project.Name
		if processIndex >= 0 && processIndex < len(project.Processes) {
			context += " / " + project.Processes[processIndex].Name
		}
	}
	value := "RUNP"
	if context != "" {
		value += "  " + context
	}
	if lipgloss.Width(value)+1+lipgloss.Width(counts) > width {
		value = "RUNP"
	}
	gap := max(width-lipgloss.Width(value)-lipgloss.Width(counts), 1)
	return appHeaderStyle.Width(width).MaxHeight(1).Render(value + strings.Repeat(" ", gap) + counts)
}

func logPaneTitle(snapshot controller.Snapshot, projectIndex, processIndex int) string {
	if projectIndex < 0 || projectIndex >= len(snapshot.Projects) {
		return "LIVE LOG"
	}
	project := snapshot.Projects[projectIndex]
	if processIndex < 0 || processIndex >= len(project.Processes) {
		return "LIVE LOG · " + strings.ToUpper(project.Name)
	}
	return "LIVE LOG · " + strings.ToUpper(project.Name) + " / " + strings.ToUpper(project.Processes[processIndex].Name)
}

func renderDashboard(snapshot controller.Snapshot, projectIndex, processIndex int, preview string, width, height int) string {
	geometry := dashboardLayout(width, height)
	header := renderAppHeader(snapshot, projectIndex, processIndex, width)
	footerLine := appFooterStyle.Width(width).MaxHeight(1).Render(footer(width))
	logTitle := logPaneTitle(snapshot, projectIndex, processIndex)
	var body string
	switch geometry.mode {
	case dashboardWide:
		projectWidth := max(geometry.projectWidth-paneFrameWidth-2*paneHorizontalPadding, 1)
		processWidth := max(geometry.processWidth-paneFrameWidth-2*paneHorizontalPadding, 1)
		projects := renderVisibleLines(projectRows(snapshot, projectIndex), projectIndex, projectWidth, max(geometry.bodyHeight-paneFrameWidth-1, 1))
		processes := renderVisibleLines(currentProcessRows(snapshot, projectIndex, processIndex, processWidth), processIndex, processWidth, max(geometry.processHeight-paneFrameWidth-2, 1))
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			renderPane("PROJECTS", projects, geometry.projectWidth, geometry.bodyHeight),
			renderPane("PROCESSES", processColumns(processWidth)+"\n"+processes, geometry.processWidth, geometry.bodyHeight),
			renderPane(logTitle, preview, geometry.logWidth, geometry.logHeight),
		)
	case dashboardMedium:
		processWidth := max(geometry.processWidth-paneFrameWidth-2*paneHorizontalPadding, 1)
		rows, selectedLine := groupedProcessRows(snapshot, projectIndex, processIndex, processWidth)
		processes := renderVisibleLines(rows, selectedLine, processWidth, max(geometry.processHeight-paneFrameWidth-2, 1))
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			renderPane("PROCESSES", processColumns(processWidth)+"\n"+processes, geometry.processWidth, geometry.bodyHeight),
			renderPane(logTitle, preview, geometry.logWidth, geometry.logHeight),
		)
	default:
		project := ""
		if projectIndex >= 0 && projectIndex < len(snapshot.Projects) {
			project = " · " + strings.ToUpper(snapshot.Projects[projectIndex].Name)
		}
		processWidth := max(width-paneFrameWidth-2*paneHorizontalPadding, 1)
		processes := renderVisibleLines(currentProcessRows(snapshot, projectIndex, processIndex, processWidth), processIndex, processWidth, max(geometry.processHeight-paneFrameWidth-2, 1))
		body = lipgloss.JoinVertical(lipgloss.Left,
			renderPane("PROCESSES"+project, processColumns(processWidth)+"\n"+processes, width, geometry.processHeight),
			renderPane(logTitle, preview, width, geometry.logHeight),
		)
	}
	return fitScreen(lipgloss.JoinVertical(lipgloss.Left, header, body, footerLine), width, height)
}

func footer(width int) string {
	full := "↑↓ Project  ←→ Process  Enter Log  s Start  k Stop  r Restart  c Clear log  g Project  a Add  e Edit  q Quit"
	if lipgloss.Width(full) <= width {
		return full
	}

	compact := "↑↓ Project  ←→ Process  Enter Log  s k r c g a e q"
	if lipgloss.Width(compact) <= width {
		return compact
	}

	return "↑↓ ←→ Enter s k r c g a e q"
}
