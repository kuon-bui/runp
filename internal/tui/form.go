package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	"runp/internal/config"
)

type formKind uint8

const (
	projectForm formKind = iota
	processForm

	fieldName          = "Name"
	fieldDirectory     = "Directory"
	fieldArgs          = "Args"
	fieldEnvKey        = "EnvKey"
	fieldEnvValue      = "EnvValue"
	fieldHealthType    = "HealthType"
	fieldRestartPolicy = "RestartPolicy"

	toggleShell     = "Shell"
	toggleAutostart = "Autostart"
)

type formSection uint8

const (
	basicSection formSection = iota
	environmentSection
	healthSection
	restartSection
	loggingSection
)

var processSections = []formSection{
	basicSection,
	environmentSection,
	healthSection,
	restartSection,
	loggingSection,
}

func (s formSection) String() string {
	switch s {
	case environmentSection:
		return "Environment"
	case healthSection:
		return "Health"
	case restartSection:
		return "Restart"
	case loggingSection:
		return "Logging"
	default:
		return "Basic"
	}
}

type formField struct {
	label   string
	display string
	section formSection
	input   textinput.Model
}

type formToggle struct {
	label   string
	display string
	section formSection
}

type editForm struct {
	kind         formKind
	base         config.Config
	projectIndex int
	processIndex int
	fields       []formField
	toggles      []formToggle
	booleans     map[string]bool
	focus        int
	workingEnv   map[string]string
	err          error
	width        int
	height       int
	creating     bool
}

func (f *editForm) sections() []formSection {
	if f.kind == projectForm {
		return []formSection{basicSection}
	}
	return processSections
}

func (f *editForm) focusLabels() []string {
	labels := make([]string, 0, len(f.fields)+len(f.toggles))
	for _, section := range f.sections() {
		for _, field := range f.fields {
			if field.section == section {
				labels = append(labels, field.label)
			}
		}
		for _, toggle := range f.toggles {
			if toggle.section == section {
				labels = append(labels, toggle.label)
			}
		}
	}
	return labels
}

func (f *editForm) fieldIndex(label string) int {
	for index := range f.fields {
		if f.fields[index].label == label {
			return index
		}
	}
	return -1
}

func (f *editForm) activeSection() formSection {
	label := f.focusLabel()
	if index := f.fieldIndex(label); index >= 0 {
		return f.fields[index].section
	}
	for _, toggle := range f.toggles {
		if toggle.label == label {
			return toggle.section
		}
	}
	return basicSection
}

func (f *editForm) focusLabel() string {
	return f.focusLabels()[f.focus]
}

func newProjectForm(cfg config.Config, projectIndex int) (*editForm, error) {
	copy, err := cloneConfig(cfg)
	if err != nil {
		return nil, err
	}
	creating := projectIndex < 0
	project := config.Project{}
	if projectIndex >= 0 {
		if projectIndex >= len(copy.Projects) {
			return nil, fmt.Errorf("project index out of range")
		}
		project = copy.Projects[projectIndex]
	} else {
		projectIndex = len(copy.Projects)
		copy.Projects = append(copy.Projects, project)
	}
	form := &editForm{
		kind:         projectForm,
		base:         copy,
		projectIndex: projectIndex,
		processIndex: -1,
		booleans:     map[string]bool{toggleAutostart: project.Autostart},
		toggles:      []formToggle{{label: toggleAutostart, display: toggleAutostart, section: basicSection}},
		width:        defaultTerminalWidth,
		height:       defaultTerminalHeight,
		creating:     creating,
	}
	form.addField(basicSection, fieldName, fieldName, project.Name)
	form.addField(basicSection, fieldDirectory, fieldDirectory, project.Directory)
	form.focusFirst()
	return form, nil
}

func newProcessForm(cfg config.Config, projectIndex, processIndex int) (*editForm, error) {
	copy, err := cloneConfig(cfg)
	if err != nil {
		return nil, err
	}
	if projectIndex < 0 || projectIndex >= len(copy.Projects) {
		return nil, fmt.Errorf("project index out of range")
	}
	creating := processIndex < 0
	item := config.Process{}
	if processIndex >= 0 {
		if processIndex >= len(copy.Projects[projectIndex].Processes) {
			return nil, fmt.Errorf("process index out of range")
		}
		item = copy.Projects[projectIndex].Processes[processIndex]
	} else {
		processIndex = len(copy.Projects[projectIndex].Processes)
		copy.Projects[projectIndex].Processes = append(copy.Projects[projectIndex].Processes, item)
	}
	form := &editForm{
		kind:         processForm,
		base:         copy,
		projectIndex: projectIndex,
		processIndex: processIndex,
		booleans:     map[string]bool{toggleShell: item.Shell, toggleAutostart: item.Autostart},
		workingEnv:   cloneMap(item.Env),
		width:        defaultTerminalWidth,
		height:       defaultTerminalHeight,
		creating:     creating,
	}
	args := []byte("[]")
	if item.Args != nil {
		args, _ = json.Marshal(item.Args)
	}
	form.addField(basicSection, fieldName, fieldName, item.Name)
	form.addField(basicSection, "Command", "Command", item.Command)
	form.addField(basicSection, fieldArgs, "Arguments", string(args))
	form.addField(basicSection, fieldDirectory, fieldDirectory, item.Directory)
	form.addField(basicSection, "DependsOn", "Depends on", strings.Join(item.DependsOn, ", "))
	form.addField(basicSection, "StopTimeout", "Stop timeout", durationString(item.StopTimeout))
	form.addField(environmentSection, fieldEnvKey, "Variable", "")
	form.addField(environmentSection, fieldEnvValue, "Value", "")
	form.fields[len(form.fields)-1].input.EchoMode = textinput.EchoPassword
	form.fields[len(form.fields)-1].input.EchoCharacter = '•'
	form.addField(environmentSection, "EnvFile", "Environment file", item.EnvFile)
	form.addField(healthSection, fieldHealthType, "Health type", item.Health.Type)
	form.addField(healthSection, "HealthURL", "URL", item.Health.URL)
	form.addField(healthSection, "HealthAddress", "Address", item.Health.Address)
	form.addField(healthSection, "HealthTimeout", "Timeout", durationString(item.Health.Timeout))
	form.addField(healthSection, "HealthInterval", "Interval", durationString(item.Health.Interval))
	form.addField(restartSection, fieldRestartPolicy, "Policy", item.Restart.Policy)
	form.addField(restartSection, "RestartMaxAttempts", "Max attempts", intString(item.Restart.MaxAttempts))
	form.addField(restartSection, "RestartWindow", "Window", durationString(item.Restart.Window))
	form.addField(restartSection, "InitialBackoff", "Initial backoff", durationString(item.Restart.InitialBackoff))
	form.addField(restartSection, "MaxBackoff", "Max backoff", durationString(item.Restart.MaxBackoff))
	form.addField(loggingSection, "LogMaxSizeMB", "Max size (MB)", intString(item.Log.MaxSizeMB))
	form.addField(loggingSection, "LogMaxFiles", "Max files", intString(item.Log.MaxFiles))
	form.addField(loggingSection, "LogBufferLines", "Buffer lines", intString(item.Log.BufferLines))
	form.toggles = []formToggle{
		{label: toggleShell, display: toggleShell, section: basicSection},
		{label: toggleAutostart, display: toggleAutostart, section: basicSection},
	}
	form.focusFirst()
	return form, nil
}

func cloneConfig(cfg config.Config) (config.Config, error) {
	data, err := json.Marshal(cfg)
	if err != nil {
		return config.Config{}, err
	}
	var result config.Config
	if err := json.Unmarshal(data, &result); err != nil {
		return config.Config{}, err
	}
	return result, nil
}

func cloneMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func (f *editForm) addField(section formSection, label, display, value string) {
	input := textinput.New()
	input.Prompt = ""
	input.SetWidth(50)
	input.SetValue(value)
	f.fields = append(f.fields, formField{
		label: label, display: display, section: section, input: input,
	})
}

func (f *editForm) focusFirst() {
	if index := f.fieldIndex(f.focusLabel()); index >= 0 {
		_ = f.fields[index].input.Focus()
	}
}

func (f *editForm) set(label, value string) {
	for index := range f.fields {
		if f.fields[index].label == label {
			f.fields[index].input.SetValue(value)
			return
		}
	}
}

func (f *editForm) value(label string) string {
	for _, field := range f.fields {
		if field.label == label {
			return field.input.Value()
		}
	}
	return ""
}

func (f *editForm) toggle(label string) {
	f.booleans[label] = !f.booleans[label]
}

func (f *editForm) update(msg tea.Msg) tea.Cmd {
	key, isKey := msg.(tea.KeyPressMsg)
	if isKey {
		if f.kind == processForm && f.focusLabel() == fieldEnvValue && key.Code == tea.KeyEnter {
			f.setEnvValue()
			return nil
		}
		if f.kind == processForm && f.focusLabel() == fieldEnvKey && key.Code == 'x' && key.Mod == tea.ModCtrl {
			f.deleteEnvKey()
			return nil
		}
		if key.Code == tea.KeyTab && key.Mod == tea.ModShift {
			f.moveFocus(-1)
			return nil
		}
		switch key.Code {
		case tea.KeyTab, tea.KeyDown:
			f.moveFocus(1)
			return nil
		case tea.KeyUp:
			f.moveFocus(-1)
			return nil
		case ' ':
			label := f.focusLabel()
			if label == toggleShell || label == toggleAutostart {
				f.toggle(label)
				return nil
			}
		case tea.KeyLeft:
			f.cycleEnum(-1)
			return nil
		case tea.KeyRight:
			f.cycleEnum(1)
			return nil
		}
		label := f.focusLabel()
		if label == fieldHealthType || label == fieldRestartPolicy {
			return nil
		}
	}
	index := f.fieldIndex(f.focusLabel())
	if index < 0 {
		return nil
	}
	var cmd tea.Cmd
	f.fields[index].input, cmd = f.fields[index].input.Update(msg)
	return cmd
}

func (f *editForm) setEnvValue() {
	key := strings.TrimSpace(f.value(fieldEnvKey))
	if key == "" {
		f.err = fmt.Errorf("env key must not be empty")
		return
	}
	if f.workingEnv == nil {
		f.workingEnv = make(map[string]string)
	}
	f.workingEnv[key] = f.value(fieldEnvValue)
	f.set(fieldEnvValue, "")
	f.err = nil
}

func (f *editForm) deleteEnvKey() {
	key := strings.TrimSpace(f.value(fieldEnvKey))
	delete(f.workingEnv, key)
	if len(f.workingEnv) == 0 {
		f.workingEnv = nil
	}
	f.set(fieldEnvValue, "")
}

func (f *editForm) moveFocus(delta int) {
	if index := f.fieldIndex(f.focusLabel()); index >= 0 {
		f.fields[index].input.Blur()
	}
	count := len(f.focusLabels())
	f.focus = (f.focus + delta + count) % count
	if index := f.fieldIndex(f.focusLabel()); index >= 0 {
		_ = f.fields[index].input.Focus()
	}
}

func (f *editForm) cycleEnum(delta int) {
	label := f.focusLabel()
	fieldIndex := f.fieldIndex(label)
	if fieldIndex < 0 {
		return
	}
	var values []string
	switch label {
	case fieldHealthType:
		values = []string{config.HealthProcess, config.HealthHTTP, config.HealthTCP}
	case fieldRestartPolicy:
		values = []string{config.RestartNever, config.RestartOnFailure, config.RestartAlways}
	default:
		return
	}
	current := f.fields[fieldIndex].input.Value()
	index := 0
	for candidate := range values {
		if values[candidate] == current {
			index = candidate
			break
		}
	}
	index = (index + delta + len(values)) % len(values)
	f.fields[fieldIndex].input.SetValue(values[index])
}

func (f *editForm) resize(width, height int) {
	f.width = max(width, 1)
	f.height = max(height, 1)
}

func (f *editForm) header() string {
	noun := "project"
	if f.kind == processForm {
		noun = "process"
	}
	if f.creating {
		return "New " + noun
	}
	name := strings.TrimSpace(f.value(fieldName))
	if name == "" {
		return "Edit " + noun
	}
	return "Edit " + noun + " · " + name
}

func (f *editForm) view() string {
	if f.width < 9 {
		return formHeaderStyle.MaxWidth(f.width).Render(f.header())
	}
	width := max(f.width-formOuterInset, 1)
	bodyWidth := max(width-formInnerInset, 1)
	var body string
	if f.kind == processForm && f.width >= wideFormBreakpoint {
		sidebar := f.renderSidebar(formSidebarWidth)
		panelWidth := max(bodyWidth-lipgloss.Width(sidebar)-panelGap, minimumPanelWidth)
		body = lipgloss.JoinHorizontal(lipgloss.Top, sidebar, "  ", f.renderPanel(panelWidth))
	} else {
		if f.kind == processForm {
			body = f.renderTabs(bodyWidth) + "\n\n"
		}
		body += f.renderPanel(bodyWidth)
	}

	content := formHeaderStyle.Render(f.header()) + "\n" + formMutedStyle.Render(f.activeSection().String()) + "\n\n" + body
	if f.err != nil {
		content += "\n" + formErrorStyle.Width(max(bodyWidth-2, 1)).Render(f.err.Error())
	}
	content += "\n\n" + formMutedStyle.Render(f.footer())
	return formFrameStyle.Width(width).Render(content)
}

func (f *editForm) renderSidebar(width int) string {
	sections := make([]string, 0, len(processSections))
	active := f.activeSection()
	for _, section := range processSections {
		if section == active {
			sections = append(sections, formActiveSectionStyle.Width(max(width-2, 1)).Render("▸ "+section.String()))
		} else {
			sections = append(sections, formSectionStyle.Width(max(width-2, 1)).Render("  "+section.String()))
		}
	}
	return strings.Join(sections, "\n")
}

func (f *editForm) renderTabs(_ int) string {
	sections := make([]string, 0, len(processSections))
	active := f.activeSection()
	for _, section := range processSections {
		name := section.String()
		if section == active {
			sections = append(sections, formActiveSectionStyle.Render("[ "+name+" ]"))
		} else {
			sections = append(sections, formSectionStyle.Render("  "+name+"  "))
		}
	}
	return strings.Join(sections, " ")
}

func (f *editForm) renderPanel(width int) string {
	controls := make([]string, 0)
	active := f.activeSection()
	for _, field := range f.fields {
		if field.section == active {
			controls = append(controls, f.renderField(field, width))
		}
	}
	for _, toggle := range f.toggles {
		if toggle.section == active {
			controls = append(controls, f.renderToggle(toggle))
		}
	}
	return strings.Join(controls, "\n\n")
}

func (f *editForm) renderField(field formField, width int) string {
	label := formMutedStyle.Render(field.display)
	focused := f.focusLabel() == field.label
	input := field.input
	input.SetWidth(max(width-6, 1))
	input.SetCursor(input.Position())
	value := input.View()
	if field.label == fieldHealthType || field.label == fieldRestartPolicy {
		value = "‹ " + field.input.Value() + " ›"
	}
	style := formInputStyle
	if focused {
		style = formFocusedInputStyle
	}
	result := label + "\n" + style.Width(max(width-4, 1)).Render(value)
	if field.label != fieldEnvKey {
		return result
	}
	keys := make([]string, 0, len(f.workingEnv))
	for key := range f.workingEnv {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		result += "\n" + formMutedStyle.Render(key+"=••••")
	}
	return result
}

func (f *editForm) renderToggle(toggle formToggle) string {
	marker := "○"
	style := formToggleStyle
	if f.booleans[toggle.label] {
		marker = "✓"
		style = formEnabledToggleStyle
	}
	if f.focusLabel() == toggle.label {
		style = style.Foreground(lipgloss.Color(colorAccent)).Bold(true)
	}
	return style.Render(marker + " " + toggle.display)
}

func (f *editForm) footer() string {
	footer := "Ctrl+S save  Esc cancel  Tab next"
	if f.kind == processForm && f.activeSection() == environmentSection {
		footer += "  Enter set env  Ctrl+X delete env"
	}
	return footer
}

func (f *editForm) config() (config.Config, error) {
	result, err := f.configWithoutValidation()
	if err != nil {
		return config.Config{}, err
	}
	if err := result.Validate(); err != nil {
		return config.Config{}, err
	}
	return result, nil
}

func (f *editForm) configWithoutValidation() (config.Config, error) {
	result, err := cloneConfig(f.base)
	if err != nil {
		return config.Config{}, err
	}
	if f.kind == projectForm {
		project := &result.Projects[f.projectIndex]
		project.Name = strings.TrimSpace(f.value(fieldName))
		project.Directory = strings.TrimSpace(f.value(fieldDirectory))
		project.Autostart = f.booleans[toggleAutostart]
		return result, nil
	}

	item := &result.Projects[f.projectIndex].Processes[f.processIndex]
	item.Name = strings.TrimSpace(f.value(fieldName))
	item.Command = f.value("Command")
	item.Shell = f.booleans[toggleShell]
	item.Directory = strings.TrimSpace(f.value(fieldDirectory))
	item.EnvFile = strings.TrimSpace(f.value("EnvFile"))
	item.Autostart = f.booleans[toggleAutostart]
	if err := parseJSONField("args", f.value(fieldArgs), &item.Args); err != nil {
		return config.Config{}, err
	}
	if item.Args == nil {
		item.Args = nil
	} else if len(item.Args) == 0 && f.value(fieldArgs) == "[]" && f.base.Projects[f.projectIndex].Processes[f.processIndex].Args == nil {
		item.Args = nil
	}
	if item.Shell && len(item.Args) > 0 {
		return config.Config{}, fmt.Errorf("args must be empty when shell is true")
	}
	item.Env = cloneMap(f.workingEnv)
	item.DependsOn = splitUnique(f.value("DependsOn"))
	if len(item.DependsOn) == 0 && f.base.Projects[f.projectIndex].Processes[f.processIndex].DependsOn == nil {
		item.DependsOn = nil
	}
	item.Health.Type = strings.TrimSpace(f.value(fieldHealthType))
	item.Health.URL = strings.TrimSpace(f.value("HealthURL"))
	item.Health.Address = strings.TrimSpace(f.value("HealthAddress"))
	if item.Health.Timeout, err = parseDurationField("health timeout", f.value("HealthTimeout")); err != nil {
		return config.Config{}, err
	}
	if item.Health.Interval, err = parseDurationField("health interval", f.value("HealthInterval")); err != nil {
		return config.Config{}, err
	}
	item.Restart.Policy = strings.TrimSpace(f.value(fieldRestartPolicy))
	if item.Restart.MaxAttempts, err = parseIntField("restart max attempts", f.value("RestartMaxAttempts")); err != nil {
		return config.Config{}, err
	}
	if item.Restart.Window, err = parseDurationField("restart window", f.value("RestartWindow")); err != nil {
		return config.Config{}, err
	}
	if item.Restart.InitialBackoff, err = parseDurationField("initial backoff", f.value("InitialBackoff")); err != nil {
		return config.Config{}, err
	}
	if item.Restart.MaxBackoff, err = parseDurationField("max backoff", f.value("MaxBackoff")); err != nil {
		return config.Config{}, err
	}
	if item.Log.MaxSizeMB, err = parseIntField("log max size", f.value("LogMaxSizeMB")); err != nil {
		return config.Config{}, err
	}
	if item.Log.MaxFiles, err = parseIntField("log max files", f.value("LogMaxFiles")); err != nil {
		return config.Config{}, err
	}
	if item.Log.BufferLines, err = parseIntField("log buffer lines", f.value("LogBufferLines")); err != nil {
		return config.Config{}, err
	}
	if item.StopTimeout, err = parseDurationField("stop timeout", f.value("StopTimeout")); err != nil {
		return config.Config{}, err
	}
	return result, nil
}

func parseJSONField(label, value string, destination any) error {
	if strings.TrimSpace(value) == "" {
		value = "null"
	}
	if err := json.Unmarshal([]byte(value), destination); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	return nil
}

func parseDurationField(label, value string) (config.Duration, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("%s: invalid duration", label)
	}
	return config.Duration(parsed), nil
}

func parseIntField(label, value string) (int, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid integer", label)
	}
	return parsed, nil
}

func splitUnique(value string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func durationString(value config.Duration) string {
	if value == 0 {
		return ""
	}
	return time.Duration(value).String()
}

func intString(value int) string {
	if value == 0 {
		return ""
	}
	return strconv.Itoa(value)
}
