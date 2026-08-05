package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

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
	ClearLog       func(string, string)
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
	clearLog
	saveConfig
	saveCritical
	deleteProcess
	deleteProject
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
	shortcuts     bool
	addMenuIndex  int
	busy          bool
	err           error
	log           *logView
	preview       logPreview
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
	model := Model{
		services: services,
		width:    defaultTerminalWidth,
		height:   defaultTerminalHeight,
		preview:  newLogPreview(1, 1),
	}
	model.refresh()
	model.refreshPreview()
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
		m.resizePreview()
		if m.log != nil {
			m.log.resize(m.width, m.height)
			m.log.refresh(m.services)
		}
		if m.form != nil {
			m.form.resize(m.width, m.height)
		}
	case runtimeEventMsg:
		beforeProject, beforeProcess, beforeSelected := m.selected()
		m.snapshot = controller.Event(msg).Snapshot
		m.clampSelection()
		afterProject, afterProcess, afterSelected := m.selected()
		if beforeSelected != afterSelected || beforeProject != afterProject || beforeProcess != afterProcess {
			m.refreshPreview()
		}
		return m, waitRuntime(m.services.RuntimeEvents)
	case logEventMsg:
		event := logstore.Event(msg)
		if m.preview.matches(event) {
			m.preview.refresh(m.services)
		}
		if m.log != nil {
			if event.Project == m.log.project && event.Process == m.log.process {
				m.log.refresh(m.services)
			}
		}
		return m, waitLogs(m.services.LogEvents)
	case logstore.Event:
		if m.preview.matches(msg) {
			m.preview.refresh(m.services)
		}
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
		m.refreshPreview()
		if msg.action == shutdown && msg.err == nil {
			return m, tea.Quit
		}
	case tea.KeyPressMsg:
		if msg.Code == 'c' && msg.Mod == tea.ModCtrl {
			return m.requestShutdown()
		}
		if m.err != nil {
			if msg.Code == tea.KeyEnter || msg.Code == tea.KeyEscape {
				m.err = nil
				if m.form != nil {
					m.form.err = nil
				}
			}
			return m, nil
		}
		if m.shortcuts {
			if msg.Code == '?' || msg.Code == tea.KeyEscape {
				m.shortcuts = false
			}
			return m, nil
		}
		if msg.Code == '?' && m.form == nil && m.pending == noAction && !m.busy &&
			!m.projectMenu && !m.addMenu && (m.log == nil || !m.log.search) {
			m.shortcuts = true
			return m, nil
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
		if m.log != nil {
			if msg.Code == 'c' && !m.log.search {
				m.pending = clearLog
				return m, nil
			}
			if msg.Code == tea.KeyEscape && !m.log.search {
				m.log = nil
				return m, nil
			}
			return m, m.log.update(msg, m.services)
		}
		if m.busy {
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
			case 'd':
				m.projectMenu = false
				m.pending = deleteProject
			case tea.KeyEscape, 'g':
				m.projectMenu = false
			}
			return m, nil
		}
		if m.addMenu {
			code := msg.Code
			switch code {
			case tea.KeyUp:
				m.addMenuIndex = max(m.addMenuIndex-1, 0)
				return m, nil
			case tea.KeyDown:
				m.addMenuIndex = min(m.addMenuIndex+1, 2)
				return m, nil
			case tea.KeyEnter:
				code = []rune{'p', 'o', tea.KeyEscape}[m.addMenuIndex]
			}
			switch code {
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
		beforeProject, beforeProcess, beforeSelected := m.selected()
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
		case 'c':
			m.pending = clearLog
		case 'g':
			m.projectMenu = true
		case 'a':
			m.addMenu = true
			m.addMenuIndex = 0
		case 'e':
			if err := m.openProcessForm(m.processIndex); err != nil {
				m.err = err
			}
		case 'd':
			m.pending = deleteProcess
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
		afterProject, afterProcess, afterSelected := m.selected()
		if beforeSelected != afterSelected || beforeProject != afterProject || beforeProcess != afterProcess {
			m.refreshPreview()
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
	m.shortcuts = false
	if m.hasActiveProcesses() {
		m.pending = shutdown
		return m, nil
	}
	m.busy = true
	return m, m.actionCommand(shutdown)
}

func (m Model) View() tea.View {
	content := renderDashboard(m.snapshot, m.projectIndex, m.processIndex, m.preview.render(), m.width, m.height)
	if m.log != nil {
		content = m.log.render()
	}
	if m.form != nil {
		content = composeOverlay(lipgloss.NewStyle().Faint(true).Render(content), m.form.view(), m.width, m.height)
	}
	if m.projectMenu {
		content = composeOverlay(content, renderProjectMenu(), m.width, m.height)
	}
	if m.addMenu {
		content = composeOverlay(content, renderAddMenu(m.addMenuIndex), m.width, m.height)
	}
	if m.pending != noAction {
		x, y, width, height := m.logPaneBounds()
		content = composeOverlayIn(content, renderConfirmation(m.pending), m.width, m.height, x, y, width, height)
	}
	if m.busy {
		x, y, width, height := m.logPaneBounds()
		content = composeOverlayIn(content, renderBusy(), m.width, m.height, x, y, width, height)
	}
	if m.err != nil {
		content = composeOverlay(content, renderOperationError(m.err), m.width, m.height)
	}
	if m.shortcuts {
		content = composeOverlay(content, renderShortcuts(), m.width, m.height)
	}
	view := tea.NewView(fitScreen(content, m.width, m.height))
	view.AltScreen = true
	return view
}

func (m Model) logPaneBounds() (x, y, width, height int) {
	if m.log != nil {
		return 0, 0, m.width, m.height
	}
	geometry := dashboardLayout(m.width, m.height)
	x, y = 0, 1
	switch geometry.mode {
	case dashboardWide:
		x = geometry.projectWidth + geometry.processWidth
	case dashboardMedium:
		x = geometry.processWidth
	case dashboardNarrow:
		y += geometry.processHeight
	}
	return x, y, geometry.logWidth, geometry.logHeight
}

func (m *Model) resizePreview() {
	geometry := dashboardLayout(m.width, m.height)
	m.preview.resize(
		max(geometry.logWidth-paneFrameWidth-2*paneHorizontalPadding, 1),
		geometry.logBodyHeight,
	)
}

func (m *Model) refreshPreview() {
	m.resizePreview()
	project, name, ok := m.selected()
	if !ok {
		m.preview.show("", "", m.services)
		return
	}
	m.preview.show(project, name, m.services)
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
	case clearLog:
		if !selected || m.services.ClearLog == nil {
			return func() tea.Msg { return operationDoneMsg{action: pending, err: errors.New("no process selected")} }
		}
		return operationCommand(pending, func(context.Context) error {
			m.services.ClearLog(project, name)
			return nil
		})
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
	case deleteProcess:
		if !selected || m.services.Config == nil || m.services.SaveConfig == nil || m.services.StopProcess == nil {
			return func() tea.Msg {
				return operationDoneMsg{action: pending, err: errors.New("process delete unavailable")}
			}
		}
		return operationCommand(pending, func(ctx context.Context) error {
			if err := m.services.StopProcess(ctx, project, name); err != nil {
				return err
			}
			cfg, err := removeProcess(m.services.Config(), project, name)
			if err != nil {
				return err
			}
			return m.services.SaveConfig(cfg)
		})
	case deleteProject:
		if selectedProject == "" || m.services.Config == nil || m.services.SaveConfig == nil || m.services.StopProject == nil {
			return func() tea.Msg {
				return operationDoneMsg{action: pending, err: errors.New("project delete unavailable")}
			}
		}
		return operationCommand(pending, func(ctx context.Context) error {
			if err := m.services.StopProject(ctx, selectedProject); err != nil {
				return err
			}
			cfg, err := removeProject(m.services.Config(), selectedProject)
			if err != nil {
				return err
			}
			return m.services.SaveConfig(cfg)
		})
	default:
		return nil
	}
}

func removeProcess(cfg config.Config, project, name string) (config.Config, error) {
	next, err := cloneConfig(cfg)
	if err != nil {
		return config.Config{}, err
	}
	for projectIndex := range next.Projects {
		if next.Projects[projectIndex].Name != project {
			continue
		}
		items := next.Projects[projectIndex].Processes
		for processIndex := range items {
			if items[processIndex].Name != name {
				continue
			}
			next.Projects[projectIndex].Processes = append(items[:processIndex], items[processIndex+1:]...)
			for remainingIndex := range next.Projects[projectIndex].Processes {
				dependencies := next.Projects[projectIndex].Processes[remainingIndex].DependsOn
				kept := dependencies[:0]
				for _, dependency := range dependencies {
					if dependency != name {
						kept = append(kept, dependency)
					}
				}
				next.Projects[projectIndex].Processes[remainingIndex].DependsOn = kept
			}
			return next, nil
		}
		return config.Config{}, fmt.Errorf("project %q has no process %q", project, name)
	}
	return config.Config{}, fmt.Errorf("project %q not found", project)
}

func removeProject(cfg config.Config, name string) (config.Config, error) {
	next, err := cloneConfig(cfg)
	if err != nil {
		return config.Config{}, err
	}
	for index := range next.Projects {
		if next.Projects[index].Name == name {
			next.Projects = append(next.Projects[:index], next.Projects[index+1:]...)
			return next, nil
		}
	}
	return config.Config{}, fmt.Errorf("project %q not found", name)
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
