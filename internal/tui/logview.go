package tui

import (
	"fmt"
	"regexp"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"runp/internal/logstore"
)

const (
	logHeaderFooterHeight = 3
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
}

func newLogView(project, process string, width, height int) logView {
	input := textinput.New()
	input.Prompt = "/ "
	input.SetWidth(max(1, width-searchPromptWidth))
	return logView{
		project: project,
		process: process,
		follow:  true,
		viewport: viewport.New(
			viewport.WithWidth(max(1, width)),
			viewport.WithHeight(max(1, height-logHeaderFooterHeight)),
		),
		input: input,
	}
}

func (l *logView) resize(width, height int) {
	l.viewport.SetWidth(max(1, width))
	l.viewport.SetHeight(max(1, height-logHeaderFooterHeight))
	l.input.SetWidth(max(1, width-searchPromptWidth))
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
	lines := make([]string, 0, len(records))
	for _, record := range records {
		stream := "OUT"
		if record.Stream == logstore.Stderr {
			stream = "ERR"
		}
		lines = append(lines, fmt.Sprintf("%s %s %s", record.At.Local().Format(logTimeFormat), stream, record.Text))
	}
	l.viewport.SetContentLines(lines)
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
	header := fmt.Sprintf("%s / %s  %s  %s", l.project, l.process, stream, mode)
	if l.query != "" {
		header += "  /" + l.query
	}
	if l.search {
		return header + "\n" + l.viewport.View() + "\n" + l.input.View()
	}
	return header + "\n" + l.viewport.View() + "\nEsc back  f follow  t stream  / search  n/N match"
}
