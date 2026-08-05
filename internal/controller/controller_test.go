package controller_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"runp/internal/config"
	"runp/internal/controller"
	"runp/internal/logstore"
	"runp/internal/process"
)

func TestControllerHelper(t *testing.T) {
	mode := os.Getenv("RUNP_CONTROLLER_HELPER")
	if mode == "" {
		return
	}
	if marker := os.Getenv("RUNP_START_MARKER"); marker != "" {
		if err := os.WriteFile(marker, nil, 0o600); err != nil {
			os.Exit(2)
		}
	}
	switch mode {
	case "wait":
		time.Sleep(24 * time.Hour)
	case "crash-once":
		marker := os.Getenv("RUNP_CRASH_MARKER")
		if _, err := os.Stat(marker); os.IsNotExist(err) {
			time.Sleep(500 * time.Millisecond)
			if err := os.WriteFile(marker, nil, 0o600); err != nil {
				os.Exit(2)
			}
			os.Exit(1)
		}
		time.Sleep(24 * time.Hour)
	}
}

func controllerProcess(name string, dependencies ...string) config.Process {
	return config.Process{
		Name:      name,
		Command:   os.Args[0],
		Args:      []string{"-test.run=^TestControllerHelper$", "--"},
		Env:       map[string]string{"RUNP_CONTROLLER_HELPER": "wait"},
		DependsOn: dependencies,
		Health: config.HealthConfig{
			Type:     "process",
			Interval: config.Duration(5 * time.Millisecond),
			Timeout:  config.Duration(time.Second),
		},
	}
}

func newTestController(t *testing.T, projects ...config.Project) (*controller.Controller, *process.Manager) {
	t.Helper()
	cfg := config.Default()
	cfg.Projects = projects
	logs := logstore.New(t.TempDir(), time.Millisecond)
	manager := process.NewManager(logs)
	control, err := controller.New(cfg, manager)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = control.ForceShutdown(ctx)
		_ = logs.Close()
	})
	return control, manager
}

func TestStartProcessOrdersDependencies(t *testing.T) {
	dir := t.TempDir()
	control, manager := newTestController(t, config.Project{
		Name:      "shop",
		Directory: dir,
		Processes: []config.Process{
			controllerProcess("db"),
			controllerProcess("api", "db"),
			controllerProcess("web", "api"),
			controllerProcess("worker"),
		},
	})
	if err := control.StartProcess(context.Background(), "shop", "web"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"db", "api", "web"} {
		snapshot, ok := manager.Snapshot(process.Key{Project: "shop", Process: name})
		if !ok || snapshot.State != process.Running {
			t.Fatalf("%s = %#v %v", name, snapshot, ok)
		}
	}
	if _, ok := manager.Snapshot(process.Key{Project: "shop", Process: "worker"}); ok {
		t.Fatal("unrelated worker started")
	}
}

func TestStopPrerequisiteStopsDependents(t *testing.T) {
	dir := t.TempDir()
	control, manager := newTestController(t, config.Project{
		Name:      "shop",
		Directory: dir,
		Processes: []config.Process{
			controllerProcess("db"),
			controllerProcess("api", "db"),
			controllerProcess("web", "api"),
		},
	})
	if err := control.StartProcess(context.Background(), "shop", "web"); err != nil {
		t.Fatal(err)
	}
	if err := control.StopProcess(context.Background(), "shop", "db"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"web", "api", "db"} {
		snapshot, _ := manager.Snapshot(process.Key{Project: "shop", Process: name})
		if snapshot.State != process.Stopped {
			t.Fatalf("%s = %#v", name, snapshot)
		}
	}
}

func TestAutostart(t *testing.T) {
	dir := t.TempDir()
	db := controllerProcess("db")
	db.Autostart = true
	control, manager := newTestController(t,
		config.Project{
			Name:      "partial",
			Directory: dir,
			Processes: []config.Process{db, controllerProcess("api")},
		},
		config.Project{
			Name:      "all",
			Directory: dir,
			Autostart: true,
			Processes: []config.Process{controllerProcess("db"), controllerProcess("api", "db")},
		},
	)
	if err := control.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertManagerState(t, manager, "partial", "db", process.Running)
	if _, ok := manager.Snapshot(process.Key{Project: "partial", Process: "api"}); ok {
		t.Fatal("non-autostart process started")
	}
	assertManagerState(t, manager, "all", "db", process.Running)
	assertManagerState(t, manager, "all", "api", process.Running)
}

func TestStartConfiguredProjectOnlyStartsItsAutostartProcesses(t *testing.T) {
	dir := t.TempDir()
	db := controllerProcess("db")
	db.Autostart = true
	other := controllerProcess("worker")
	other.Autostart = true
	control, manager := newTestController(t,
		config.Project{Name: "shop", Directory: dir, Processes: []config.Process{db, controllerProcess("api")}},
		config.Project{Name: "tools", Directory: dir, Processes: []config.Process{other}},
	)
	if err := control.StartConfiguredProject(context.Background(), "shop"); err != nil {
		t.Fatal(err)
	}
	assertManagerState(t, manager, "shop", "db", process.Running)
	for _, key := range []process.Key{{Project: "shop", Process: "api"}, {Project: "tools", Process: "worker"}} {
		if _, ok := manager.Snapshot(key); ok {
			t.Fatalf("non-selected autostart process started: %#v", key)
		}
	}
}

func TestStartProjectStartsIndependentLevelConcurrently(t *testing.T) {
	dir := t.TempDir()
	dbMarker := filepath.Join(dir, "db.started")
	workerMarker := filepath.Join(dir, "worker.started")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := os.Stat(dbMarker); err != nil {
			http.Error(w, "db missing", http.StatusServiceUnavailable)
			return
		}
		if _, err := os.Stat(workerMarker); err != nil {
			http.Error(w, "worker missing", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	db := controllerProcess("db")
	db.Env["RUNP_START_MARKER"] = dbMarker
	db.Health = config.HealthConfig{Type: "http", URL: server.URL, Interval: config.Duration(time.Millisecond), Timeout: config.Duration(time.Second)}
	worker := controllerProcess("worker")
	worker.Env["RUNP_START_MARKER"] = workerMarker
	worker.Health = db.Health
	control, manager := newTestController(t, config.Project{
		Name:      "shop",
		Directory: dir,
		Processes: []config.Process{db, worker, controllerProcess("api", "db")},
	})
	if err := control.StartProject(context.Background(), "shop"); err != nil {
		t.Fatal(err)
	}
	assertManagerState(t, manager, "shop", "db", process.Running)
	assertManagerState(t, manager, "shop", "worker", process.Running)
	assertManagerState(t, manager, "shop", "api", process.Running)
	dbSnapshot, _ := manager.Snapshot(process.Key{Project: "shop", Process: "db"})
	workerSnapshot, _ := manager.Snapshot(process.Key{Project: "shop", Process: "worker"})
	apiSnapshot, _ := manager.Snapshot(process.Key{Project: "shop", Process: "api"})
	if apiSnapshot.StartedAt.Before(dbSnapshot.StartedAt) || apiSnapshot.StartedAt.Before(workerSnapshot.StartedAt) {
		t.Fatalf("start times db=%s worker=%s api=%s", dbSnapshot.StartedAt, workerSnapshot.StartedAt, apiSnapshot.StartedAt)
	}
}

func TestRestartPrerequisiteRestoresOnlyRunningDependents(t *testing.T) {
	dir := t.TempDir()
	control, manager := newTestController(t, config.Project{
		Name:      "shop",
		Directory: dir,
		Processes: []config.Process{
			controllerProcess("db"),
			controllerProcess("api", "db"),
			controllerProcess("web", "api"),
		},
	})
	if err := control.StartProcess(context.Background(), "shop", "api"); err != nil {
		t.Fatal(err)
	}
	oldDB, _ := manager.Snapshot(process.Key{Project: "shop", Process: "db"})
	if err := control.RestartProcess(context.Background(), "shop", "db"); err != nil {
		t.Fatal(err)
	}
	newDB, _ := manager.Snapshot(process.Key{Project: "shop", Process: "db"})
	if newDB.PID == oldDB.PID {
		t.Fatalf("db PID unchanged: %d", newDB.PID)
	}
	assertManagerState(t, manager, "shop", "api", process.Running)
	if _, ok := manager.Snapshot(process.Key{Project: "shop", Process: "web"}); ok {
		t.Fatal("stopped web restored")
	}
}

func TestPrerequisiteCrashBlocksAndRestoresDependents(t *testing.T) {
	dir := t.TempDir()
	db := controllerProcess("db")
	db.Env["RUNP_CONTROLLER_HELPER"] = "crash-once"
	db.Env["RUNP_CRASH_MARKER"] = filepath.Join(dir, "db.crashed")
	db.Restart = config.RestartConfig{
		Policy:         "on-failure",
		MaxAttempts:    1,
		Window:         config.Duration(time.Minute),
		InitialBackoff: config.Duration(5 * time.Millisecond),
		MaxBackoff:     config.Duration(5 * time.Millisecond),
	}
	control, _ := newTestController(t, config.Project{
		Name:      "shop",
		Directory: dir,
		Processes: []config.Process{db, controllerProcess("api", "db")},
	})
	if err := control.StartProcess(context.Background(), "shop", "api"); err != nil {
		t.Fatal(err)
	}
	blocked := false
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event := <-control.Events():
			state := controllerState(event.Snapshot, "shop", "api")
			blocked = blocked || state == process.Blocked
			if blocked && state == process.Running {
				return
			}
		case <-deadline:
			t.Fatal("dependent was not blocked and restored")
		}
	}
}

func TestReplaceConfigRejectsActiveProcessChange(t *testing.T) {
	dir := t.TempDir()
	project := config.Project{Name: "shop", Directory: dir, Processes: []config.Process{controllerProcess("api")}}
	control, _ := newTestController(t, project)
	if err := control.StartProcess(context.Background(), "shop", "api"); err != nil {
		t.Fatal(err)
	}
	changed := config.Default()
	changedProcess := controllerProcess("api")
	changedProcess.Args = []string{"changed"}
	changed.Projects = []config.Project{{Name: "shop", Directory: dir, Processes: []config.Process{changedProcess}}}
	err := control.ReplaceConfig(changed)
	if err == nil || !strings.Contains(err.Error(), "active process") {
		t.Fatalf("error = %v", err)
	}
}

func TestReplaceConfigAllowsActiveProcessAutostartChange(t *testing.T) {
	dir := t.TempDir()
	project := config.Project{Name: "shop", Directory: dir, Processes: []config.Process{controllerProcess("api")}}
	control, _ := newTestController(t, project)
	if err := control.StartProcess(context.Background(), "shop", "api"); err != nil {
		t.Fatal(err)
	}
	changed := config.Default()
	item := controllerProcess("api")
	item.Autostart = true
	changed.Projects = []config.Project{{Name: "shop", Directory: dir, Autostart: true, Processes: []config.Process{item}}}
	if err := control.ReplaceConfig(changed); err != nil {
		t.Fatal(err)
	}
}

func controllerState(snapshot controller.Snapshot, project, name string) process.State {
	for _, item := range snapshot.Projects {
		if item.Name != project {
			continue
		}
		for _, processSnapshot := range item.Processes {
			if processSnapshot.Name == name {
				return processSnapshot.Runtime.State
			}
		}
	}
	return ""
}

func assertManagerState(t *testing.T, manager *process.Manager, project, name string, want process.State) {
	t.Helper()
	snapshot, ok := manager.Snapshot(process.Key{Project: project, Process: name})
	if !ok || snapshot.State != want {
		t.Fatalf("%s/%s = %#v %v", project, name, snapshot, ok)
	}
}
