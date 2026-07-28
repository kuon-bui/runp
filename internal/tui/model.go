package tui

import (
	"context"
	"errors"
	"reflect"

	tea "charm.land/bubbletea/v2"

	"runp/internal/config"
	"runp/internal/controller"
	"runp/internal/logstore"
	"runp/internal/process"
)

type Services struct {
	Snapshots      func() controller.Snapshot
	RuntimeEvents  <-chan controller.Event
	LogEvents      <-chan logstore.Event
	LogSnapshot    func(string, string) []logstore.Record
	LogQuery       func(string, string, logstore.Filter) []logstore.Record
	StartProcess   func(context.Context, string, string) error
	StopProcess    func(context.Context, string, string) error
	RestartProcess func(context.Context, string, string) error
	StartProject   func(context.Context, string) error
	StopProject    func(context.Context, string) error
	RestartProject func(context.Context, string) error
	Shutdown       func(context.Context) error
	ForceShutdown  func(context.Context) error
	Config         func() config.Config
	SaveConfig     func(config.Config) error
}

type action uint8

const (
	noAction action = iota
	stopProcess
	restartProcess
	shutdown
	startProject
	stopProject
	restartProject
	saveConfig
	saveCritical
)

type Model struct {
	services      Services
	snapshot      controller.Snapshot
	projectIndex  int
	processIndex  int
	width         int
	height        int
	pending       action
	projectMenu   bool
	addMenu       bool
	busy          bool
	err           error
	log           *logView
	form          *editForm
	pendingConfig config.Config
	editProject   string
	editProcess   string
	editRestart   bool
}

type runtimeEventMsg controller.Event
type logEventMsg logstore.Event
type ShutdownRequestMsg struct{}
type operationDoneMsg struct {
	action action
	err    error
}

func New(services Services) Model {
	model := Model{services: services, width: defaultTerminalWidth, height: defaultTerminalHeight}
	model.refresh()
	return model
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(waitRuntime(m.services.RuntimeEvents), waitLogs(m.services.LogEvents))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ShutdownRequestMsg:
		m.pending = noAction
		m.busy = true
		return m, m.actionCommand(shutdown)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.log != nil {
			m.log.resize(m.width, m.height)
			m.log.refresh(m.services)
		}
		if m.form != nil {
			m.form.resize(m.width, m.height)
		}
	case runtimeEventMsg:
		m.snapshot = controller.Event(msg).Snapshot
		m.clampSelection()
		return m, waitRuntime(m.services.RuntimeEvents)
	case logEventMsg:
		if m.log != nil {
			event := logstore.Event(msg)
			if event.Project == m.log.project && event.Process == m.log.process {
				m.log.refresh(m.services)
			}
		}
		return m, waitLogs(m.services.LogEvents)
	case logstore.Event:
		if m.log != nil && msg.Project == m.log.project && msg.Process == m.log.process {
			m.log.refresh(m.services)
		}
	case operationDoneMsg:
		m.busy = false
		m.err = msg.err
		if m.form != nil {
			m.form.err = msg.err
		}
		if (msg.action == saveConfig || msg.action == saveCritical) && msg.err == nil {
			m.form = nil
			m.pendingConfig = config.Config{}
		}
		m.refresh()
		if msg.action == shutdown && msg.err == nil {
			return m, tea.Quit
		}
	case tea.KeyPressMsg:
		if msg.Code == 'c' && msg.Mod == tea.ModCtrl {
			return m.requestShutdown()
		}
		if m.log != nil {
			if msg.Code == tea.KeyEscape && !m.log.search {
				m.log = nil
				return m, nil
			}
			return m, m.log.update(msg, m.services)
		}
		if m.pending != noAction {
			if msg.Code == 'y' {
				pending := m.pending
				m.pending = noAction
				m.busy = true
				return m, m.actionCommand(pending)
			}
			if msg.Code == 'n' || msg.Code == tea.KeyEscape {
				m.pending = noAction
			}
			return m, nil
		}
		if m.form != nil {
			if msg.Code == tea.KeyEscape {
				m.form = nil
				m.err = nil
				return m, nil
			}
			if msg.Code == 's' && msg.Mod == tea.ModCtrl {
				cfg, err := m.form.config()
				if err != nil {
					m.form.err = err
					m.err = err
					return m, nil
				}
				m.pendingConfig = cfg
				if m.form.kind == processForm && m.processEditCritical(cfg) {
					m.pending = saveCritical
					return m, nil
				}
				m.busy = true
				return m, m.actionCommand(saveConfig)
			}
			return m, m.form.update(msg)
		}
		if m.busy {
			return m, nil
		}
		if m.projectMenu {
			switch msg.Code {
			case 's':
				m.projectMenu = false
				m.busy = true
				return m, m.actionCommand(startProject)
			case 'k':
				m.projectMenu = false
				m.pending = stopProject
			case 'r':
				m.projectMenu = false
				m.pending = restartProject
			case 'e':
				m.projectMenu = false
				if err := m.openProjectForm(m.projectIndex); err != nil {
					m.err = err
				}
			case tea.KeyEscape, 'g':
				m.projectMenu = false
			}
			return m, nil
		}
		if m.addMenu {
			switch msg.Code {
			case 'p':
				m.addMenu = false
				if err := m.openProjectForm(-1); err != nil {
					m.err = err
				}
			case 'o':
				m.addMenu = false
				if err := m.openProcessForm(-1); err != nil {
					m.err = err
				}
			case tea.KeyEscape, 'a':
				m.addMenu = false
			}
			return m, nil
		}
		m.refresh()
		switch msg.Code {
		case tea.KeyUp:
			if m.projectIndex > 0 {
				m.projectIndex--
				m.processIndex = 0
			}
		case tea.KeyDown:
			if m.projectIndex+1 < len(m.snapshot.Projects) {
				m.projectIndex++
				m.processIndex = 0
			}
		case tea.KeyLeft:
			if m.processIndex > 0 {
				m.processIndex--
			}
		case tea.KeyRight:
			if m.processIndex+1 < m.processCount() {
				m.processIndex++
			}
		case 's':
			m.busy = true
			return m, m.startCommand()
		case 'k':
			m.pending = stopProcess
		case 'r':
			m.pending = restartProcess
		case 'g':
			m.projectMenu = true
		case 'a':
			m.addMenu = true
		case 'e':
			if err := m.openProcessForm(m.processIndex); err != nil {
				m.err = err
			}
		case tea.KeyEnter:
			project, name, ok := m.selected()
			if ok {
				view := newLogView(project, name, m.width, m.height)
				view.refresh(m.services)
				m.log = &view
			}
		case 'q':
			return m.requestShutdown()
		}
	}
	m.clampSelection()
	return m, nil
}

func (m Model) requestShutdown() (tea.Model, tea.Cmd) {
	m.log = nil
	m.form = nil
	m.projectMenu = false
	m.addMenu = false
	if m.hasActiveProcesses() {
		m.pending = shutdown
		return m, nil
	}
	m.busy = true
	return m, m.actionCommand(shutdown)
}

func (m Model) View() tea.View {
	if m.log != nil {
		view := tea.NewView(m.log.render())
		view.AltScreen = true
		return view
	}
	if m.form != nil {
		view := tea.NewView(m.form.view())
		view.AltScreen = true
		return view
	}
	content := renderDashboard(m.snapshot, m.projectIndex, m.processIndex, m.width)
	if m.pending != noAction {
		content += "\n" + confirmStyle.Render("Confirm? [y/N]")
	}
	if m.projectMenu {
		content += "\nProject: [s] start  [k] stop  [r] restart  [e] edit  [esc] cancel"
	}
	if m.addMenu {
		content += "\nAdd: [p] project  [o] process  [esc] cancel"
	}
	if m.busy {
		content += "\nStopping processes…"
	}
	if m.err != nil {
		content += "\n" + errorStyle.Render(m.err.Error())
	}
	content += "\n" + footer(m.width)
	view := tea.NewView(content)
	view.AltScreen = true
	return view
}

func (m *Model) refresh() {
	if m.services.Snapshots != nil {
		m.snapshot = m.services.Snapshots()
	}
	m.clampSelection()
}

func (m *Model) clampSelection() {
	if len(m.snapshot.Projects) == 0 {
		m.projectIndex = 0
		m.processIndex = 0
		return
	}
	m.projectIndex = min(max(m.projectIndex, 0), len(m.snapshot.Projects)-1)
	count := len(m.snapshot.Projects[m.projectIndex].Processes)
	if count == 0 {
		m.processIndex = 0
		return
	}
	m.processIndex = min(max(m.processIndex, 0), count-1)
}

func (m Model) processCount() int {
	if len(m.snapshot.Projects) == 0 {
		return 0
	}
	return len(m.snapshot.Projects[m.projectIndex].Processes)
}

func (m Model) selected() (string, string, bool) {
	if len(m.snapshot.Projects) == 0 || m.processCount() == 0 {
		return "", "", false
	}
	project := m.snapshot.Projects[m.projectIndex]
	return project.Name, project.Processes[m.processIndex].Name, true
}

func (m Model) startCommand() tea.Cmd {
	project, name, ok := m.selected()
	if !ok || m.services.StartProcess == nil {
		return func() tea.Msg { return operationDoneMsg{err: errors.New("no process selected")} }
	}
	return operationCommand(noAction, func(ctx context.Context) error {
		return m.services.StartProcess(ctx, project, name)
	})
}

func (m Model) actionCommand(pending action) tea.Cmd {
	project, name, selected := m.selected()
	selectedProject := ""
	if len(m.snapshot.Projects) > 0 {
		selectedProject = m.snapshot.Projects[m.projectIndex].Name
	}
	switch pending {
	case stopProcess:
		if !selected || m.services.StopProcess == nil {
			return func() tea.Msg { return operationDoneMsg{action: pending, err: errors.New("no process selected")} }
		}
		return operationCommand(pending, func(ctx context.Context) error { return m.services.StopProcess(ctx, project, name) })
	case restartProcess:
		if !selected || m.services.RestartProcess == nil {
			return func() tea.Msg { return operationDoneMsg{action: pending, err: errors.New("no process selected")} }
		}
		return operationCommand(pending, func(ctx context.Context) error { return m.services.RestartProcess(ctx, project, name) })
	case shutdown:
		if m.services.Shutdown == nil {
			return func() tea.Msg { return operationDoneMsg{action: pending} }
		}
		return operationCommand(pending, m.services.Shutdown)
	case startProject:
		if selectedProject == "" || m.services.StartProject == nil {
			return func() tea.Msg { return operationDoneMsg{action: pending, err: errors.New("no project selected")} }
		}
		return operationCommand(pending, func(ctx context.Context) error { return m.services.StartProject(ctx, selectedProject) })
	case stopProject:
		if selectedProject == "" || m.services.StopProject == nil {
			return func() tea.Msg { return operationDoneMsg{action: pending, err: errors.New("no project selected")} }
		}
		return operationCommand(pending, func(ctx context.Context) error { return m.services.StopProject(ctx, selectedProject) })
	case restartProject:
		if selectedProject == "" || m.services.RestartProject == nil {
			return func() tea.Msg { return operationDoneMsg{action: pending, err: errors.New("no project selected")} }
		}
		return operationCommand(pending, func(ctx context.Context) error { return m.services.RestartProject(ctx, selectedProject) })
	case saveConfig:
		if m.services.SaveConfig == nil {
			return func() tea.Msg { return operationDoneMsg{action: pending, err: errors.New("config save unavailable")} }
		}
		cfg := m.pendingConfig
		return operationCommand(pending, func(context.Context) error { return m.services.SaveConfig(cfg) })
	case saveCritical:
		if m.services.SaveConfig == nil || m.services.StopProcess == nil {
			return func() tea.Msg {
				return operationDoneMsg{action: pending, err: errors.New("critical config save unavailable")}
			}
		}
		cfg := m.pendingConfig
		project, name, restart := m.editProject, m.editProcess, m.editRestart
		return operationCommand(pending, func(ctx context.Context) error {
			if err := m.services.StopProcess(ctx, project, name); err != nil {
				return err
			}
			if err := m.services.SaveConfig(cfg); err != nil {
				return err
			}
			if restart && m.services.StartProcess != nil {
				return m.services.StartProcess(ctx, project, name)
			}
			return nil
		})
	default:
		return nil
	}
}

func operationCommand(action action, call func(context.Context) error) tea.Cmd {
	return func() tea.Msg {
		return operationDoneMsg{action: action, err: call(context.Background())}
	}
}

func (m Model) hasActiveProcesses() bool {
	for _, project := range m.snapshot.Projects {
		for _, item := range project.Processes {
			switch item.Runtime.State {
			case process.Starting, process.Running, process.Stopping, process.Restarting:
				return true
			}
		}
	}
	return false
}

func (m *Model) openProjectForm(index int) error {
	if m.services.Config == nil {
		return errors.New("config unavailable")
	}
	form, err := newProjectForm(m.services.Config(), index)
	if err != nil {
		return err
	}
	form.resize(m.width, m.height)
	m.form = form
	return nil
}

func (m *Model) openProcessForm(index int) error {
	if m.services.Config == nil {
		return errors.New("config unavailable")
	}
	if len(m.snapshot.Projects) == 0 {
		return errors.New("add project first")
	}
	form, err := newProcessForm(m.services.Config(), m.projectIndex, index)
	if err != nil {
		return err
	}
	form.resize(m.width, m.height)
	m.form = form
	if index >= 0 {
		m.editProject = m.snapshot.Projects[m.projectIndex].Name
		m.editProcess = m.snapshot.Projects[m.projectIndex].Processes[index].Name
		state := m.snapshot.Projects[m.projectIndex].Processes[index].Runtime.State
		m.editRestart = state == process.Running || state == process.Starting || state == process.Restarting
	}
	return nil
}

func (m Model) processEditCritical(next config.Config) bool {
	if m.form == nil || m.form.processIndex < 0 || m.form.projectIndex < 0 || m.form.projectIndex >= len(m.snapshot.Projects) || m.form.processIndex >= len(m.snapshot.Projects[m.form.projectIndex].Processes) {
		return false
	}
	state := m.snapshot.Projects[m.form.projectIndex].Processes[m.form.processIndex].Runtime.State
	if state != process.Running && state != process.Starting && state != process.Stopping && state != process.Restarting {
		return false
	}
	oldItem := m.form.base.Projects[m.form.projectIndex].Processes[m.form.processIndex]
	newItem := next.Projects[m.form.projectIndex].Processes[m.form.processIndex]
	oldItem.Name, newItem.Name = "", ""
	oldItem.Autostart, newItem.Autostart = false, false
	return !reflect.DeepEqual(oldItem, newItem)
}

func waitRuntime(events <-chan controller.Event) tea.Cmd {
	if events == nil {
		return nil
	}
	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			return nil
		}
		return runtimeEventMsg(event)
	}
}

func waitLogs(events <-chan logstore.Event) tea.Cmd {
	if events == nil {
		return nil
	}
	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			return nil
		}
		return logEventMsg(event)
	}
}
