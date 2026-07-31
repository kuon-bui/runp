package tui

import (
	"fmt"
	"regexp"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	"runp/internal/logstore"
)

const (
	logHeaderFooterHeight = 2
	searchPromptWidth     = 2
	logTimeFormat         = "15:04:05.000"
)

type logView struct {
	project  string
	process  string
	stream   logstore.Stream
	query    string
	follow   bool
	search   bool
	viewport viewport.Model
	input    textinput.Model
	width    int
	height   int
}

func formatLogRecords(records []logstore.Record) []string {
	lines := make([]string, 0, len(records))
	for _, record := range records {
		stream := "OUT"
		if record.Stream == logstore.Stderr {
			stream = "ERR"
		}
		lines = append(lines, fmt.Sprintf("%s %s %s", record.At.Local().Format(logTimeFormat), stream, record.Text))
	}
	return lines
}

type logPreview struct {
	project  string
	process  string
	viewport viewport.Model
}

func newLogPreview(width, height int) logPreview {
	view := viewport.New(
		viewport.WithWidth(max(width, 1)),
		viewport.WithHeight(max(height, 1)),
	)
	view.FillHeight = true
	return logPreview{viewport: view}
}

func (l *logPreview) show(project, process string, services Services) {
	l.project, l.process = project, process
	l.refresh(services)
}

func (l *logPreview) refresh(services Services) {
	var records []logstore.Record
	if l.project != "" && l.process != "" && services.LogSnapshot != nil {
		records = services.LogSnapshot(l.project, l.process)
	}
	lines := formatLogRecords(records)
	if len(lines) == 0 {
		lines = []string{"Waiting for output…"}
	}
	l.viewport.SetContentLines(lines)
	l.viewport.GotoBottom()
}

func (l *logPreview) resize(width, height int) {
	l.viewport.SetWidth(max(width, 1))
	l.viewport.SetHeight(max(height, 1))
	l.viewport.GotoBottom()
}

func (l logPreview) matches(event logstore.Event) bool {
	return event.Project == l.project && event.Process == l.process
}

func (l logPreview) render() string { return l.viewport.View() }

func newLogView(project, process string, width, height int) logView {
	input := textinput.New()
	input.Prompt = "/ "
	input.SetWidth(max(1, width-searchPromptWidth))
	return logView{
		project: project,
		process: process,
		follow:  true,
		width:   max(1, width),
		height:  max(1, height),
		viewport: viewport.New(
			viewport.WithWidth(logViewportWidth(width)),
			viewport.WithHeight(logViewportHeight(height)),
		),
		input: input,
	}
}

func (l *logView) resize(width, height int) {
	l.width = max(1, width)
	l.height = max(1, height)
	l.viewport.SetWidth(logViewportWidth(width))
	l.viewport.SetHeight(logViewportHeight(height))
	l.input.SetWidth(max(1, width-searchPromptWidth))
}

func logViewportWidth(width int) int {
	return max(1, width-paneFrameWidth-2*paneHorizontalPadding)
}

func logViewportHeight(height int) int {
	return max(1, height-logHeaderFooterHeight-paneFrameWidth-1)
}

func (l *logView) refresh(services Services) {
	var records []logstore.Record
	filter := logstore.Filter{Stream: l.stream, Query: l.query}
	if services.LogQuery != nil && (filter.Stream != "" || filter.Query != "") {
		records = services.LogQuery(l.project, l.process, filter)
	} else if services.LogSnapshot != nil {
		records = services.LogSnapshot(l.project, l.process)
		if filter.Stream != "" || filter.Query != "" {
			records = filterRecords(records, filter)
		}
	}
	l.viewport.SetContentLines(formatLogRecords(records))
	l.applyHighlights()
	if l.follow {
		l.viewport.GotoBottom()
	}
}

func filterRecords(records []logstore.Record, filter logstore.Filter) []logstore.Record {
	result := make([]logstore.Record, 0, len(records))
	query := strings.ToLower(filter.Query)
	for _, record := range records {
		if filter.Stream != "" && record.Stream != filter.Stream {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(record.Text), query) {
			continue
		}
		result = append(result, record)
	}
	return result
}

func (l *logView) applyHighlights() {
	l.viewport.ClearHighlights()
	if l.query == "" {
		return
	}
	pattern := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(l.query))
	l.viewport.SetHighlights(pattern.FindAllStringIndex(l.viewport.GetContent(), -1))
}

func (l *logView) update(msg tea.Msg, services Services) tea.Cmd {
	if l.search {
		if key, ok := msg.(tea.KeyPressMsg); ok {
			switch key.Code {
			case tea.KeyEnter:
				l.query = l.input.Value()
				l.search = false
				l.input.Blur()
				l.refresh(services)
				return nil
			case tea.KeyEscape:
				l.search = false
				l.input.Blur()
				return nil
			}
		}
		var cmd tea.Cmd
		l.input, cmd = l.input.Update(msg)
		return cmd
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.Code {
		case 'f':
			l.follow = !l.follow
			if l.follow {
				l.viewport.GotoBottom()
			}
			return nil
		case 't':
			switch l.stream {
			case "":
				l.stream = logstore.Stdout
			case logstore.Stdout:
				l.stream = logstore.Stderr
			default:
				l.stream = ""
			}
			l.refresh(services)
			return nil
		case '/':
			l.search = true
			l.input.SetValue(l.query)
			return l.input.Focus()
		case 'n':
			l.viewport.HighlightNext()
			return nil
		case 'N':
			l.viewport.HighlightPrevious()
			return nil
		case tea.KeyUp, tea.KeyPgUp:
			l.follow = false
		}
	}
	var cmd tea.Cmd
	l.viewport, cmd = l.viewport.Update(msg)
	return cmd
}

func (l logView) render() string {
	stream := "BOTH"
	if l.stream == logstore.Stdout {
		stream = "STDOUT"
	} else if l.stream == logstore.Stderr {
		stream = "STDERR"
	}
	mode := "PAUSED"
	if l.follow {
		mode = "FOLLOW"
	}
	header := appHeaderStyle.Width(l.width).MaxHeight(1).Render(fmt.Sprintf(
		"RUNP  %s / %s  %s  %s",
		strings.ToUpper(l.project), strings.ToUpper(l.process), stream, mode,
	))
	if l.query != "" {
		header = appHeaderStyle.Width(l.width).MaxHeight(1).Render(
			fmt.Sprintf("RUNP  %s / %s  %s  %s  /%s", strings.ToUpper(l.project), strings.ToUpper(l.process), stream, mode, l.query),
		)
	}
	footer := appFooterStyle.Width(l.width).MaxHeight(1).Render(
		"[Esc] Back  [f] Follow  [t] Stream  [/] Search  [n/N] Match  [c] Clear log",
	)
	if l.search {
		footer = appFooterStyle.Width(l.width).MaxHeight(1).Render(l.input.View())
	}
	body := renderPane("LOG OUTPUT", l.viewport.View(), l.width, max(l.height-logHeaderFooterHeight, 1))
	return fitScreen(
		lipgloss.JoinVertical(lipgloss.Left, header, body, footer),
		l.width, l.height,
	)
}
