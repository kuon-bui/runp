package controller

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"runp/internal/config"
	"runp/internal/process"
)

type ProcessSnapshot struct {
	Name      string
	DependsOn []string
	Runtime   process.Snapshot
}

type ProjectSnapshot struct {
	Name      string
	Directory string
	Processes []ProcessSnapshot
}

type Snapshot struct {
	Projects []ProjectSnapshot
}

type Event struct {
	Snapshot Snapshot
}

type Controller struct {
	manager *process.Manager

	mu        sync.RWMutex
	cfg       config.Config
	graphs    map[string]*graph
	processes map[process.Key]config.ResolvedProcess
	runtime   map[process.Key]process.Snapshot
	restore   map[process.Key]map[string]struct{}

	events       chan Event
	eventMu      sync.Mutex
	eventQueue   []Event
	eventClosing bool
	eventNotify  chan struct{}
	eventDone    chan struct{}
	eventOnce    sync.Once
	loopOnce     sync.Once
}

func New(cfg config.Config, manager *process.Manager) (*Controller, error) {
	graphs, processes, err := prepare(cfg)
	if err != nil {
		return nil, err
	}
	control := &Controller{
		manager:     manager,
		cfg:         cfg,
		graphs:      graphs,
		processes:   processes,
		runtime:     make(map[process.Key]process.Snapshot),
		restore:     make(map[process.Key]map[string]struct{}),
		events:      make(chan Event, 64),
		eventNotify: make(chan struct{}, 1),
		eventDone:   make(chan struct{}),
	}
	go control.deliverEvents()
	control.startEventLoop()
	return control, nil
}

func prepare(cfg config.Config) (map[string]*graph, map[process.Key]config.ResolvedProcess, error) {
	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}
	graphs := make(map[string]*graph, len(cfg.Projects))
	processes := make(map[process.Key]config.ResolvedProcess)
	for _, project := range cfg.Projects {
		g, err := newGraph(project.Processes)
		if err != nil {
			return nil, nil, fmt.Errorf("project %q: %w", project.Name, err)
		}
		graphs[project.Name] = g
		for _, item := range project.Processes {
			resolved, err := cfg.Resolve(project.Name, item.Name)
			if err != nil {
				return nil, nil, err
			}
			processes[process.Key{Project: project.Name, Process: item.Name}] = resolved
		}
	}
	return graphs, processes, nil
}

func (c *Controller) Events() <-chan Event {
	return c.events
}

func (c *Controller) Snapshot() Snapshot {
	c.mu.RLock()
	cfg := c.cfg
	runtime := make(map[process.Key]process.Snapshot, len(c.runtime))
	for key, snapshot := range c.runtime {
		runtime[key] = snapshot
	}
	c.mu.RUnlock()
	result := Snapshot{Projects: make([]ProjectSnapshot, 0, len(cfg.Projects))}
	for _, project := range cfg.Projects {
		projectSnapshot := ProjectSnapshot{Name: project.Name, Directory: project.Directory, Processes: make([]ProcessSnapshot, 0, len(project.Processes))}
		for _, item := range project.Processes {
			key := process.Key{Project: project.Name, Process: item.Name}
			processRuntime := runtime[key]
			if processRuntime.Key == (process.Key{}) {
				processRuntime = process.Snapshot{Key: key, State: process.Stopped}
			}
			projectSnapshot.Processes = append(projectSnapshot.Processes, ProcessSnapshot{
				Name:      item.Name,
				DependsOn: append([]string(nil), item.DependsOn...),
				Runtime:   processRuntime,
			})
		}
		result.Projects = append(result.Projects, projectSnapshot)
	}
	return result
}

func (c *Controller) Start(ctx context.Context) error {
	c.startEventLoop()
	c.mu.RLock()
	projects := append([]config.Project(nil), c.cfg.Projects...)
	c.mu.RUnlock()
	var result error
	for _, project := range projects {
		if project.Autostart {
			result = errors.Join(result, c.StartProject(ctx, project.Name))
			continue
		}
		for _, item := range project.Processes {
			if item.Autostart {
				result = errors.Join(result, c.StartProcess(ctx, project.Name, item.Name))
			}
		}
	}
	return result
}

func (c *Controller) StartProcess(ctx context.Context, project, name string) error {
	g, ok := c.projectGraph(project)
	if !ok {
		return fmt.Errorf("project %q not found", project)
	}
	if _, ok := g.nodes[name]; !ok {
		return fmt.Errorf("project %q has no process %q", project, name)
	}
	selected := make(map[string]struct{})
	selected[name] = struct{}{}
	for _, dependency := range g.dependencies(name) {
		selected[dependency] = struct{}{}
	}
	return c.startSelected(ctx, project, g, selected)
}

func (c *Controller) StopProcess(ctx context.Context, project, name string) error {
	g, ok := c.projectGraph(project)
	if !ok {
		return fmt.Errorf("project %q not found", project)
	}
	if _, ok := g.nodes[name]; !ok {
		return fmt.Errorf("project %q has no process %q", project, name)
	}
	selected := make(map[string]struct{})
	selected[name] = struct{}{}
	for _, dependent := range g.dependents(name) {
		selected[dependent] = struct{}{}
	}
	return c.stopSelected(ctx, project, g, selected)
}

func (c *Controller) RestartProcess(ctx context.Context, project, name string) error {
	g, ok := c.projectGraph(project)
	if !ok {
		return fmt.Errorf("project %q not found", project)
	}
	running := make(map[string]struct{})
	for _, dependent := range append(g.dependents(name), name) {
		snapshot, ok := c.manager.Snapshot(process.Key{Project: project, Process: dependent})
		if ok && (snapshot.State == process.Running || snapshot.State == process.Starting || snapshot.State == process.Restarting) {
			running[dependent] = struct{}{}
		}
	}
	if err := c.StopProcess(ctx, project, name); err != nil {
		return err
	}
	if _, wasRunning := running[name]; !wasRunning {
		return nil
	}
	return c.startSelected(ctx, project, g, running)
}

func (c *Controller) StartProject(ctx context.Context, project string) error {
	g, ok := c.projectGraph(project)
	if !ok {
		return fmt.Errorf("project %q not found", project)
	}
	return c.startSelected(ctx, project, g, g.nodes)
}

func (c *Controller) StopProject(ctx context.Context, project string) error {
	g, ok := c.projectGraph(project)
	if !ok {
		return fmt.Errorf("project %q not found", project)
	}
	return c.stopSelected(ctx, project, g, g.nodes)
}

func (c *Controller) RestartProject(ctx context.Context, project string) error {
	if err := c.StopProject(ctx, project); err != nil {
		return err
	}
	return c.StartProject(ctx, project)
}

func (c *Controller) startSelected(ctx context.Context, project string, g *graph, selected map[string]struct{}) error {
	for _, level := range g.levels() {
		names := filterLevel(level, selected)
		if err := c.parallel(names, func(name string) error {
			c.mu.RLock()
			cfg, ok := c.processes[process.Key{Project: project, Process: name}]
			c.mu.RUnlock()
			if !ok {
				return fmt.Errorf("process %s/%s not found", project, name)
			}
			return c.manager.Start(ctx, cfg)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) stopSelected(ctx context.Context, project string, g *graph, selected map[string]struct{}) error {
	for _, level := range g.reverseLevels() {
		names := filterLevel(level, selected)
		if err := c.parallel(names, func(name string) error {
			return c.manager.Stop(ctx, process.Key{Project: project, Process: name})
		}); err != nil {
			return err
		}
	}
	return nil
}

func filterLevel(level []string, selected map[string]struct{}) []string {
	result := make([]string, 0, len(level))
	for _, name := range level {
		if _, ok := selected[name]; ok {
			result = append(result, name)
		}
	}
	return result
}

func (c *Controller) parallel(names []string, action func(string) error) error {
	results := make(chan error, len(names))
	var waits sync.WaitGroup
	for _, name := range names {
		name := name
		waits.Add(1)
		go func() {
			defer waits.Done()
			results <- action(name)
		}()
	}
	waits.Wait()
	close(results)
	var result error
	for err := range results {
		result = errors.Join(result, err)
	}
	return result
}

func (c *Controller) ReplaceConfig(cfg config.Config) error {
	graphs, processes, err := prepare(cfg)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.validateReplacement(processes); err != nil {
		return err
	}
	c.cfg = cfg
	c.graphs = graphs
	c.processes = processes
	return nil
}

func (c *Controller) ValidateReplacement(cfg config.Config) error {
	_, processes, err := prepare(cfg)
	if err != nil {
		return err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.validateReplacement(processes)
}

func (c *Controller) PersistConfig(cfg config.Config, persist func() error) error {
	graphs, processes, err := prepare(cfg)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.validateReplacement(processes); err != nil {
		return err
	}
	if err := persist(); err != nil {
		return err
	}
	c.cfg = cfg
	c.graphs = graphs
	c.processes = processes
	return nil
}

func (c *Controller) validateReplacement(processes map[process.Key]config.ResolvedProcess) error {
	for key, oldCfg := range c.processes {
		snapshot, ok := c.manager.Snapshot(key)
		if !ok || snapshot.State == process.Stopped || snapshot.State == process.Failed || snapshot.State == process.Blocked {
			continue
		}
		newCfg, exists := processes[key]
		if !exists || !sameRuntimeConfig(oldCfg, newCfg) {
			return fmt.Errorf("active process %s/%s changed or removed", key.Project, key.Process)
		}
	}
	return nil
}

func sameRuntimeConfig(oldCfg, newCfg config.ResolvedProcess) bool {
	oldCfg.Autostart = false
	newCfg.Autostart = false
	return reflect.DeepEqual(oldCfg, newCfg)
}

func (c *Controller) Shutdown(ctx context.Context) error {
	err := c.manager.Shutdown(ctx)
	c.closeEvents()
	return err
}

func (c *Controller) ForceShutdown(ctx context.Context) error {
	err := c.manager.ForceShutdown(ctx)
	c.closeEvents()
	return err
}

func (c *Controller) projectGraph(project string) (*graph, bool) {
	c.mu.RLock()
	g, ok := c.graphs[project]
	c.mu.RUnlock()
	return g, ok
}

func (c *Controller) startEventLoop() {
	c.loopOnce.Do(func() {
		go func() {
			for event := range c.manager.Events() {
				c.handleManagerEvent(event)
			}
		}()
	})
}

func (c *Controller) handleManagerEvent(event process.Event) {
	c.mu.Lock()
	c.runtime[event.Snapshot.Key] = event.Snapshot
	c.mu.Unlock()
	c.emit(c.Snapshot())

	switch event.Snapshot.State {
	case process.Failed, process.Restarting:
		c.blockDependents(event.Snapshot.Key)
	case process.Running:
		c.restoreDependents(event.Snapshot.Key)
	}
}

func (c *Controller) blockDependents(key process.Key) {
	g, ok := c.projectGraph(key.Project)
	if !ok {
		return
	}
	selected := make(map[string]struct{})
	for _, name := range g.dependents(key.Process) {
		snapshot, exists := c.manager.Snapshot(process.Key{Project: key.Project, Process: name})
		if exists && (snapshot.State == process.Running || snapshot.State == process.Starting || snapshot.State == process.Restarting) {
			selected[name] = struct{}{}
		}
	}
	if len(selected) == 0 {
		return
	}
	c.mu.Lock()
	if c.restore[key] == nil {
		c.restore[key] = make(map[string]struct{})
	}
	for name := range selected {
		c.restore[key][name] = struct{}{}
	}
	c.mu.Unlock()

	for _, level := range g.reverseLevels() {
		for _, name := range filterLevel(level, selected) {
			_ = c.manager.Block(context.Background(), process.Key{Project: key.Project, Process: name}, fmt.Sprintf("dependency %s unavailable", key.Process))
		}
	}
}

func (c *Controller) restoreDependents(key process.Key) {
	c.mu.Lock()
	selected := c.restore[key]
	delete(c.restore, key)
	g := c.graphs[key.Project]
	c.mu.Unlock()
	if len(selected) == 0 || g == nil {
		return
	}
	_ = c.startSelected(context.Background(), key.Project, g, selected)
}

func (c *Controller) emit(snapshot Snapshot) {
	c.eventMu.Lock()
	if !c.eventClosing {
		c.eventQueue = append(c.eventQueue, Event{Snapshot: snapshot})
	}
	c.eventMu.Unlock()
	select {
	case c.eventNotify <- struct{}{}:
	default:
	}
}

func (c *Controller) deliverEvents() {
	defer close(c.eventDone)
	for {
		c.eventMu.Lock()
		if len(c.eventQueue) > 0 {
			event := c.eventQueue[0]
			c.eventQueue = c.eventQueue[1:]
			c.eventMu.Unlock()
			c.events <- event
			continue
		}
		closing := c.eventClosing
		c.eventMu.Unlock()
		if closing {
			close(c.events)
			return
		}
		<-c.eventNotify
	}
}

func (c *Controller) closeEvents() {
	c.eventOnce.Do(func() {
		c.eventMu.Lock()
		c.eventClosing = true
		c.eventMu.Unlock()
		select {
		case c.eventNotify <- struct{}{}:
		default:
		}
		<-c.eventDone
	})
}
