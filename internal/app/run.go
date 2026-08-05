package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"runp/internal/config"
	"runp/internal/controller"
	"runp/internal/logstore"
	"runp/internal/process"
	"runp/internal/tui"
)

const (
	logBatchInterval = 50 * time.Millisecond
	shutdownTimeout  = 10 * time.Second
)

var runProgram = func(program *tea.Program) error {
	_, err := program.Run()
	return err
}

func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) (result error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = errors.Join(result, fmt.Errorf("panic: %v\n%s", recovered, debug.Stack()))
		}
	}()

	flags := flag.NewFlagSet("runp", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "config file path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	if *configPath == "" {
		path, err := config.CurrentPath()
		if err != nil {
			return fmt.Errorf("config path: %w", err)
		}
		*configPath = path
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	dataDir, err := config.DataDir()
	if err != nil {
		return fmt.Errorf("data directory: %w", err)
	}
	logs := logstore.New(filepath.Join(dataDir, "logs"), logBatchInterval)
	manager := process.NewManager(logs)
	control, err := controller.New(cfg, manager)
	if err != nil {
		_ = logs.Close()
		return fmt.Errorf("controller: %w", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		result = errors.Join(result, control.Shutdown(cleanupCtx), logs.Close())
	}()

	services := tui.Services{
		Snapshots:      control.Snapshot,
		RuntimeEvents:  control.Events(),
		LogEvents:      logs.Events(),
		LogSnapshot:    logs.Snapshot,
		LogQuery:       logs.Query,
		ClearLog:       logs.Clear,
		StartProcess:   control.StartProcess,
		StopProcess:    control.StopProcess,
		RestartProcess: control.RestartProcess,
		StartProject:   control.StartProject,
		StopProject:    control.StopProject,
		RestartProject: control.RestartProject,
		Shutdown:       control.Shutdown,
		ForceShutdown:  control.ForceShutdown,
		Config:         func() config.Config { return cfg },
		SaveConfig: func(next config.Config) error {
			return saveReplacement(*configPath, control, &cfg, next)
		},
	}
	createProject, err := startConfigured(ctx, control, cfg, stdin, stdout)
	if err != nil {
		return fmt.Errorf("startup: %w", err)
	}
	services.OpenProjectForm = createProject
	program := tea.NewProgram(tui.New(services), tea.WithInput(stdin), tea.WithOutput(stdout), tea.WithContext(ctx), tea.WithoutSignals())
	stopSignals := handleSignals(program, control)
	defer stopSignals()
	return runProgram(program)
}

func startConfigured(ctx context.Context, control *controller.Controller, cfg config.Config, stdin io.Reader, stdout io.Writer) (bool, error) {
	var choice startChoice
	if shouldPromptStartMode(stdin, stdout, cfg) {
		selected, err := promptStartProject(ctx, cfg, stdin, stdout)
		if err != nil {
			return false, err
		}
		choice = selected
	}
	if choice.project != "" {
		return false, control.StartConfiguredProject(ctx, choice.project)
	}
	return choice.createProject, control.Start(ctx)
}

func shouldPromptStartMode(stdin io.Reader, stdout io.Writer, cfg config.Config) bool {
	input, inputOK := stdin.(*os.File)
	output, outputOK := stdout.(*os.File)
	return inputOK && outputOK && isCharDevice(input) && isCharDevice(output)
}

func isCharDevice(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func promptStartProject(ctx context.Context, cfg config.Config, stdin io.Reader, stdout io.Writer) (startChoice, error) {
	model, err := runStartPrompt(&startPrompt{cfg: cfg}, ctx, stdin, stdout)
	if err != nil {
		return startChoice{}, err
	}
	return model.choice, nil
}

type startChoice struct {
	project       string
	createProject bool
}

var runStartPrompt = func(model *startPrompt, ctx context.Context, stdin io.Reader, stdout io.Writer) (*startPrompt, error) {
	program := tea.NewProgram(model, tea.WithInput(stdin), tea.WithOutput(stdout), tea.WithContext(ctx), tea.WithoutSignals())
	result, err := program.Run()
	if err != nil {
		return nil, err
	}
	prompt, ok := result.(*startPrompt)
	if !ok {
		return nil, fmt.Errorf("unexpected start prompt model %T", result)
	}
	return prompt, nil
}

type startPrompt struct {
	cfg          config.Config
	modeCursor   int
	projectStage bool
	projectIndex int
	choice       startChoice
}

func (p *startPrompt) Init() tea.Cmd { return nil }

func (p *startPrompt) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.Code {
		case tea.KeyUp:
			p.move(-1)
		case tea.KeyDown:
			p.move(1)
		case tea.KeyEscape:
			p.choice = startChoice{}
			return p, tea.Quit
		case tea.KeyEnter:
			return p.selectItem()
		case '1':
			if !p.projectStage {
				p.choice = startChoice{}
				return p, tea.Quit
			}
		case '2':
			if !p.projectStage {
				p.projectStage = true
				return p, nil
			}
		}
	}
	return p, nil
}

func (p *startPrompt) move(delta int) {
	limit := 2
	if p.projectStage {
		limit = len(p.cfg.Projects)
	}
	if limit == 0 {
		return
	}
	if p.projectStage {
		p.projectIndex = (p.projectIndex + delta + limit) % limit
		return
	}
	p.modeCursor = (p.modeCursor + delta + limit) % limit
}

func (p *startPrompt) selectItem() (tea.Model, tea.Cmd) {
	if p.projectStage {
		if p.projectIndex >= 0 && p.projectIndex < len(p.cfg.Projects) {
			p.choice.project = p.cfg.Projects[p.projectIndex].Name
		} else if len(p.cfg.Projects) == 0 {
			p.choice.createProject = true
		}
		return p, tea.Quit
	}
	if p.modeCursor == 0 {
		p.choice = startChoice{}
		return p, tea.Quit
	}
	if len(p.cfg.Projects) == 0 {
		p.choice.createProject = true
		return p, tea.Quit
	}
	p.projectStage = true
	return p, nil
}

func (p *startPrompt) View() tea.View {
	if p.projectStage {
		lines := []string{"Start one project", ""}
		if len(p.cfg.Projects) == 0 {
			lines = append(lines, "› Create project")
		}
		for index, project := range p.cfg.Projects {
			marker := "  "
			if index == p.projectIndex {
				marker = "› "
			}
			lines = append(lines, marker+project.Name)
		}
		lines = append(lines, "", "↑/↓ move · Enter select · Esc autostart")
		return tea.NewView(joinLines(lines))
	}
	lines := []string{"Start mode", ""}
	items := []string{"Multiple projects (autostart)", "One project (autostart)"}
	for index, item := range items {
		marker := "  "
		if index == p.modeCursor {
			marker = "› "
		}
		lines = append(lines, marker+item)
	}
	lines = append(lines, "", "↑/↓ move · Enter select · Esc autostart")
	return tea.NewView(joinLines(lines))
}

func joinLines(lines []string) string {
	var result strings.Builder
	for index, line := range lines {
		if index > 0 {
			result.WriteString("\n")
		}
		result.WriteString(line)
	}
	return result.String()
}

func saveReplacement(path string, control *controller.Controller, current *config.Config, next config.Config) error {
	if err := control.PersistConfig(next, func() error { return config.Save(path, next) }); err != nil {
		return err
	}
	*current = next
	return nil
}
