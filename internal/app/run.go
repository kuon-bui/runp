package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"runtime/debug"
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
	program := tea.NewProgram(tui.New(services), tea.WithInput(stdin), tea.WithOutput(stdout), tea.WithContext(ctx), tea.WithoutSignals())
	stopSignals := handleSignals(program, control)
	defer stopSignals()
	if err := control.Start(ctx); err != nil {
		return fmt.Errorf("autostart: %w", err)
	}
	return runProgram(program)
}

func saveReplacement(path string, control *controller.Controller, current *config.Config, next config.Config) error {
	if err := control.PersistConfig(next, func() error { return config.Save(path, next) }); err != nil {
		return err
	}
	*current = next
	return nil
}
