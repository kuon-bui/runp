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

	"runp/internal/config"
)

type formKind uint8

const (
	projectForm formKind = iota
	processForm
)

type formField struct {
	label string
	input textinput.Model
}

type editForm struct {
	kind         formKind
	base         config.Config
	projectIndex int
	processIndex int
	fields       []formField
	booleans     map[string]bool
	focus        int
	workingEnv   map[string]string
	err          error
}

func (f *editForm) focusLabels() []string {
	labels := make([]string, 0, len(f.fields)+len(f.booleans))
	for _, field := range f.fields {
		labels = append(labels, field.label)
	}
	for _, label := range []string{"Shell", "Autostart"} {
		if _, ok := f.booleans[label]; ok {
			labels = append(labels, label)
		}
	}
	return labels
}

func (f *editForm) focusLabel() string {
	return f.focusLabels()[f.focus]
}

func newProjectForm(cfg config.Config, projectIndex int) (*editForm, error) {
	copy, err := cloneConfig(cfg)
	if err != nil {
		return nil, err
	}
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
		booleans:     map[string]bool{"Autostart": project.Autostart},
	}
	form.addField("Name", project.Name)
	form.addField("Directory", project.Directory)
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
		booleans:     map[string]bool{"Shell": item.Shell, "Autostart": item.Autostart},
		workingEnv:   cloneMap(item.Env),
	}
	args := []byte("[]")
	if item.Args != nil {
		args, _ = json.Marshal(item.Args)
	}
	form.addField("Name", item.Name)
	form.addField("Command", item.Command)
	form.addField("Args", string(args))
	form.addField("Directory", item.Directory)
	form.addField("EnvKey", "")
	form.addField("EnvValue", "")
	form.fields[len(form.fields)-1].input.EchoMode = textinput.EchoPassword
	form.fields[len(form.fields)-1].input.EchoCharacter = '•'
	form.addField("EnvFile", item.EnvFile)
	form.addField("DependsOn", strings.Join(item.DependsOn, ", "))
	form.addField("HealthType", item.Health.Type)
	form.addField("HealthURL", item.Health.URL)
	form.addField("HealthAddress", item.Health.Address)
	form.addField("HealthTimeout", durationString(item.Health.Timeout))
	form.addField("HealthInterval", durationString(item.Health.Interval))
	form.addField("RestartPolicy", item.Restart.Policy)
	form.addField("RestartMaxAttempts", intString(item.Restart.MaxAttempts))
	form.addField("RestartWindow", durationString(item.Restart.Window))
	form.addField("InitialBackoff", durationString(item.Restart.InitialBackoff))
	form.addField("MaxBackoff", durationString(item.Restart.MaxBackoff))
	form.addField("LogMaxSizeMB", intString(item.Log.MaxSizeMB))
	form.addField("LogMaxFiles", intString(item.Log.MaxFiles))
	form.addField("LogBufferLines", intString(item.Log.BufferLines))
	form.addField("StopTimeout", durationString(item.StopTimeout))
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

func (f *editForm) addField(label, value string) {
	input := textinput.New()
	input.Prompt = ""
	input.SetWidth(50)
	input.SetValue(value)
	f.fields = append(f.fields, formField{label: label, input: input})
}

func (f *editForm) focusFirst() {
	if len(f.fields) > 0 {
		_ = f.fields[0].input.Focus()
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
		if f.kind == processForm && f.focusLabel() == "EnvValue" && key.Code == tea.KeyEnter {
			f.setEnvValue()
			return nil
		}
		if f.kind == processForm && f.focusLabel() == "EnvKey" && key.Code == 'x' && key.Mod == tea.ModCtrl {
			f.deleteEnvKey()
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
			if label == "Shell" || label == "Autostart" {
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
		if key.Code == tea.KeyTab && key.Mod == tea.ModShift {
			f.moveFocus(-1)
			return nil
		}
	}
	if f.focus >= len(f.fields) {
		return nil
	}
	var cmd tea.Cmd
	f.fields[f.focus].input, cmd = f.fields[f.focus].input.Update(msg)
	return cmd
}

func (f *editForm) setEnvValue() {
	key := strings.TrimSpace(f.value("EnvKey"))
	if key == "" {
		f.err = fmt.Errorf("env key must not be empty")
		return
	}
	if f.workingEnv == nil {
		f.workingEnv = make(map[string]string)
	}
	f.workingEnv[key] = f.value("EnvValue")
	f.set("EnvValue", "")
	f.err = nil
}

func (f *editForm) deleteEnvKey() {
	key := strings.TrimSpace(f.value("EnvKey"))
	delete(f.workingEnv, key)
	if len(f.workingEnv) == 0 {
		f.workingEnv = nil
	}
	f.set("EnvValue", "")
}

func (f *editForm) moveFocus(delta int) {
	if f.focus < len(f.fields) {
		f.fields[f.focus].input.Blur()
	}
	count := len(f.focusLabels())
	f.focus = (f.focus + delta + count) % count
	if f.focus < len(f.fields) {
		_ = f.fields[f.focus].input.Focus()
	}
}

func (f *editForm) cycleEnum(delta int) {
	if f.focus >= len(f.fields) {
		return
	}
	label := f.focusLabel()
	var values []string
	switch label {
	case "HealthType":
		values = []string{"process", "http", "tcp"}
	case "RestartPolicy":
		values = []string{"never", "on-failure", "always"}
	default:
		return
	}
	current := f.fields[f.focus].input.Value()
	index := 0
	for candidate := range values {
		if values[candidate] == current {
			index = candidate
			break
		}
	}
	index = (index + delta + len(values)) % len(values)
	f.fields[f.focus].input.SetValue(values[index])
}

func (f *editForm) view() string {
	var content strings.Builder
	if f.kind == projectForm {
		content.WriteString("Project form\n")
	} else {
		content.WriteString("Process form\n")
	}
	for index, field := range f.fields {
		marker := "  "
		if index == f.focus {
			marker = "› "
		}
		content.WriteString(marker)
		content.WriteString(field.label)
		content.WriteString(": ")
		if field.label == "EnvKey" {
			content.WriteString(field.input.View())
			keys := make([]string, 0, len(f.workingEnv))
			for key := range f.workingEnv {
				keys = append(keys, key)
			}
			if len(keys) > 0 {
				sort.Strings(keys)
				content.WriteString(" [")
				content.WriteString(strings.Join(keys, "=••••, "))
				content.WriteString("=••••]")
			}
		} else {
			content.WriteString(field.input.View())
		}
		content.WriteByte('\n')
	}
	booleanIndex := len(f.fields)
	for _, label := range []string{"Shell", "Autostart"} {
		if _, ok := f.booleans[label]; ok {
			marker := "  "
			if booleanIndex == f.focus {
				marker = "› "
			}
			content.WriteString(fmt.Sprintf("%s%s: %t\n", marker, label, f.booleans[label]))
			booleanIndex++
		}
	}
	if f.err != nil {
		content.WriteString(errorStyle.Render(f.err.Error()))
		content.WriteByte('\n')
	}
	content.WriteString("Ctrl+S save  Esc cancel  Tab next")
	if f.kind == processForm {
		content.WriteString("  Enter set env  Ctrl+X delete env")
	}
	return content.String()
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
		project.Name = strings.TrimSpace(f.value("Name"))
		project.Directory = strings.TrimSpace(f.value("Directory"))
		project.Autostart = f.booleans["Autostart"]
		return result, nil
	}

	item := &result.Projects[f.projectIndex].Processes[f.processIndex]
	item.Name = strings.TrimSpace(f.value("Name"))
	item.Command = f.value("Command")
	item.Shell = f.booleans["Shell"]
	item.Directory = strings.TrimSpace(f.value("Directory"))
	item.EnvFile = strings.TrimSpace(f.value("EnvFile"))
	item.Autostart = f.booleans["Autostart"]
	if err := parseJSONField("args", f.value("Args"), &item.Args); err != nil {
		return config.Config{}, err
	}
	if item.Args == nil {
		item.Args = nil
	} else if len(item.Args) == 0 && f.value("Args") == "[]" && f.base.Projects[f.projectIndex].Processes[f.processIndex].Args == nil {
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
	item.Health.Type = strings.TrimSpace(f.value("HealthType"))
	item.Health.URL = strings.TrimSpace(f.value("HealthURL"))
	item.Health.Address = strings.TrimSpace(f.value("HealthAddress"))
	if item.Health.Timeout, err = parseDurationField("health timeout", f.value("HealthTimeout")); err != nil {
		return config.Config{}, err
	}
	if item.Health.Interval, err = parseDurationField("health interval", f.value("HealthInterval")); err != nil {
		return config.Config{}, err
	}
	item.Restart.Policy = strings.TrimSpace(f.value("RestartPolicy"))
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
