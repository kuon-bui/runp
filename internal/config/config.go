package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *Duration) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		return fmt.Errorf("duration must be a string")
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("duration must be a string: %w", err)
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", value, err)
	}
	if parsed < 0 {
		return fmt.Errorf("duration must not be negative")
	}
	*d = Duration(parsed)
	return nil
}

type Config struct {
	Version  int       `json:"version"`
	Defaults Defaults  `json:"defaults"`
	Projects []Project `json:"projects"`
}

type Defaults struct {
	StopTimeout Duration      `json:"stopTimeout"`
	Log         LogConfig     `json:"log"`
	Restart     RestartConfig `json:"restart"`
}

type LogConfig struct {
	MaxSizeMB   int `json:"maxSizeMB"`
	MaxFiles    int `json:"maxFiles"`
	BufferLines int `json:"bufferLines"`
}

type RestartConfig struct {
	Policy         string   `json:"policy,omitempty"`
	MaxAttempts    int      `json:"maxAttempts,omitempty"`
	Window         Duration `json:"window,omitempty"`
	InitialBackoff Duration `json:"initialBackoff,omitempty"`
	MaxBackoff     Duration `json:"maxBackoff,omitempty"`
}

type HealthConfig struct {
	Type     string   `json:"type,omitempty"`
	URL      string   `json:"url,omitempty"`
	Address  string   `json:"address,omitempty"`
	Timeout  Duration `json:"timeout,omitempty"`
	Interval Duration `json:"interval,omitempty"`
}

type Project struct {
	Name      string    `json:"name"`
	Directory string    `json:"directory"`
	Autostart bool      `json:"autostart"`
	Processes []Process `json:"processes"`
}

type Process struct {
	Name        string            `json:"name"`
	Command     string            `json:"command"`
	Args        []string          `json:"args,omitempty"`
	Shell       bool              `json:"shell"`
	Directory   string            `json:"directory,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	EnvFile     string            `json:"envFile,omitempty"`
	Autostart   bool              `json:"autostart"`
	StopTimeout Duration          `json:"stopTimeout,omitempty"`
	DependsOn   []string          `json:"dependsOn,omitempty"`
	Health      HealthConfig      `json:"health,omitempty"`
	Restart     RestartConfig     `json:"restart,omitempty"`
	Log         LogConfig         `json:"log,omitempty"`
}

type ResolvedProcess struct {
	ProjectName string
	Name        string
	Directory   string
	Command     string
	Args        []string
	Shell       bool
	Env         []string
	DependsOn   []string
	Autostart   bool
	StopTimeout time.Duration
	Health      HealthConfig
	Restart     RestartConfig
	Log         LogConfig
}

func Default() Config {
	return Config{
		Version: 1,
		Defaults: Defaults{
			StopTimeout: Duration(5 * time.Second),
			Log: LogConfig{
				MaxSizeMB:   10,
				MaxFiles:    5,
				BufferLines: 10000,
			},
			Restart: RestartConfig{
				MaxAttempts:    5,
				Window:         Duration(time.Minute),
				InitialBackoff: Duration(time.Second),
				MaxBackoff:     Duration(30 * time.Second),
			},
		},
		Projects: []Project{},
	}
}

func SafeName(name string) string {
	var result strings.Builder
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			result.WriteRune(r)
		} else {
			result.WriteByte('_')
		}
	}
	safe := result.String()
	if safe == "." || safe == ".." {
		return "_" + safe
	}
	return safe
}

func (c Config) Validate() error {
	if c.Version != 1 {
		return fmt.Errorf("version: must equal 1")
	}
	defaults := effectiveDefaults(c.Defaults)
	if err := validateDefaults(defaults); err != nil {
		return err
	}
	projectNames := make(map[string]struct{}, len(c.Projects))
	projectSafeNames := make(map[string]string, len(c.Projects))
	for projectIndex, project := range c.Projects {
		path := fmt.Sprintf("projects[%d]", projectIndex)
		if strings.TrimSpace(project.Name) == "" {
			return fmt.Errorf("%s.name: must not be empty", path)
		}
		if _, exists := projectNames[project.Name]; exists {
			return fmt.Errorf("%s.name: duplicate project %q", path, project.Name)
		}
		projectNames[project.Name] = struct{}{}
		safe := SafeName(project.Name)
		if previous, exists := projectSafeNames[safe]; exists {
			return fmt.Errorf("%s.name: safe name %q collides with project %q", path, safe, previous)
		}
		projectSafeNames[safe] = project.Name
		root, err := filepath.Abs(project.Directory)
		if err != nil {
			return fmt.Errorf("%s.directory: %w", path, err)
		}
		if err := requireDirectory(root); err != nil {
			return fmt.Errorf("%s.directory: %w", path, err)
		}
		if err := validateProject(path, root, project, defaults); err != nil {
			return err
		}
	}
	return nil
}

func validateDefaults(defaults Defaults) error {
	if defaults.StopTimeout <= 0 {
		return fmt.Errorf("defaults.stopTimeout: must be positive")
	}
	if defaults.Log.MaxSizeMB <= 0 || defaults.Log.MaxFiles <= 0 || defaults.Log.BufferLines <= 0 {
		return fmt.Errorf("defaults.log: limits must be positive")
	}
	if defaults.Restart.MaxAttempts <= 0 || defaults.Restart.Window <= 0 || defaults.Restart.InitialBackoff <= 0 || defaults.Restart.MaxBackoff <= 0 {
		return fmt.Errorf("defaults.restart: limits must be positive")
	}
	return nil
}

func validateProject(path, root string, project Project, defaults Defaults) error {
	names := make(map[string]struct{}, len(project.Processes))
	safeNames := make(map[string]string, len(project.Processes))
	for index, process := range project.Processes {
		processPath := fmt.Sprintf("%s.processes[%d]", path, index)
		if strings.TrimSpace(process.Name) == "" {
			return fmt.Errorf("%s.name: must not be empty", processPath)
		}
		if _, exists := names[process.Name]; exists {
			return fmt.Errorf("%s.name: duplicate process %q", processPath, process.Name)
		}
		names[process.Name] = struct{}{}
		safe := SafeName(process.Name)
		if previous, exists := safeNames[safe]; exists {
			return fmt.Errorf("%s.name: safe name %q collides with process %q", processPath, safe, previous)
		}
		safeNames[safe] = process.Name
		if strings.TrimSpace(process.Command) == "" {
			return fmt.Errorf("%s.command: must not be empty", processPath)
		}
		if process.Shell && len(process.Args) != 0 {
			return fmt.Errorf("%s.args: must be empty when shell is true", processPath)
		}
		directory := root
		if process.Directory != "" {
			directory = process.Directory
			if !filepath.IsAbs(directory) {
				directory = filepath.Join(root, directory)
			}
		}
		if err := requireDirectory(directory); err != nil {
			return fmt.Errorf("%s.directory: %w", processPath, err)
		}
		if err := validateHealth(processPath+".health", process.Health); err != nil {
			return err
		}
		if err := validateRestart(processPath+".restart", process.Restart, defaults.Restart); err != nil {
			return err
		}
		if process.StopTimeout < 0 {
			return fmt.Errorf("%s.stopTimeout: must be positive", processPath)
		}
		log := mergeLog(defaults.Log, process.Log)
		if log.MaxSizeMB <= 0 || log.MaxFiles <= 0 || log.BufferLines <= 0 {
			return fmt.Errorf("%s.log: limits must be positive", processPath)
		}
	}
	for _, process := range project.Processes {
		for _, dependency := range process.DependsOn {
			if _, exists := names[dependency]; !exists {
				return fmt.Errorf("%s.%s.dependsOn: missing process %q", project.Name, process.Name, dependency)
			}
		}
	}
	return validateAcyclic(project)
}

func validateHealth(path string, health HealthConfig) error {
	typeName := health.Type
	if typeName == "" {
		typeName = "process"
	}
	if health.Timeout < 0 || health.Interval < 0 {
		return fmt.Errorf("%s: durations must be positive", path)
	}
	switch typeName {
	case "process":
		return nil
	case "http":
		parsed, err := url.ParseRequestURI(health.URL)
		if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" {
			return fmt.Errorf("%s.url: must be a valid http or https URL", path)
		}
		return nil
	case "tcp":
		_, port, err := net.SplitHostPort(health.Address)
		if err != nil {
			return fmt.Errorf("%s.address: must be valid host:port", path)
		}
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > 65535 {
			return fmt.Errorf("%s.address: must be valid host:port", path)
		}
		return nil
	default:
		return fmt.Errorf("%s.type: must be process, http, or tcp", path)
	}
}

func validateRestart(path string, restart, defaults RestartConfig) error {
	policy := restart.Policy
	if policy == "" {
		policy = "never"
	}
	if policy != "never" && policy != "on-failure" && policy != "always" {
		return fmt.Errorf("%s.policy: must be never, on-failure, or always", path)
	}
	merged := mergeRestart(defaults, restart)
	if merged.MaxAttempts <= 0 || merged.Window <= 0 || merged.InitialBackoff <= 0 || merged.MaxBackoff <= 0 {
		return fmt.Errorf("%s: limits must be positive", path)
	}
	return nil
}

func validateAcyclic(project Project) error {
	dependencies := make(map[string][]string, len(project.Processes))
	for _, process := range project.Processes {
		dependencies[process.Name] = process.DependsOn
	}
	colors := make(map[string]uint8, len(project.Processes))
	var visit func(string) error
	visit = func(name string) error {
		if colors[name] == 1 {
			return fmt.Errorf("%s.%s.dependsOn: dependency cycle", project.Name, name)
		}
		if colors[name] == 2 {
			return nil
		}
		colors[name] = 1
		for _, dependency := range dependencies[name] {
			if colors[dependency] == 1 {
				return fmt.Errorf("%s.%s.dependsOn: dependency cycle through %q", project.Name, dependency, name)
			}
			if err := visit(dependency); err != nil {
				return err
			}
		}
		colors[name] = 2
		return nil
	}
	for _, process := range project.Processes {
		if err := visit(process.Name); err != nil {
			return err
		}
	}
	return nil
}

func (c Config) Resolve(projectName, processName string) (ResolvedProcess, error) {
	if err := c.Validate(); err != nil {
		return ResolvedProcess{}, err
	}
	defaults := effectiveDefaults(c.Defaults)
	for _, project := range c.Projects {
		if project.Name != projectName {
			continue
		}
		root, _ := filepath.Abs(project.Directory)
		for _, process := range project.Processes {
			if process.Name != processName {
				continue
			}
			directory := root
			if process.Directory != "" {
				directory = process.Directory
				if !filepath.IsAbs(directory) {
					directory = filepath.Join(root, directory)
				}
			}
			fileEnv := map[string]string{}
			if process.EnvFile != "" {
				envPath := process.EnvFile
				if !filepath.IsAbs(envPath) {
					envPath = filepath.Join(root, envPath)
				}
				file, err := os.Open(envPath)
				if err != nil {
					return ResolvedProcess{}, fmt.Errorf("%s.%s.envFile: %w", projectName, processName, err)
				}
				fileEnv, err = ParseEnv(file)
				closeErr := file.Close()
				if err != nil {
					return ResolvedProcess{}, fmt.Errorf("%s.%s.envFile: %w", projectName, processName, err)
				}
				if closeErr != nil {
					return ResolvedProcess{}, fmt.Errorf("%s.%s.envFile: %w", projectName, processName, closeErr)
				}
			}
			health := process.Health
			if health.Type == "" {
				health.Type = "process"
			}
			if health.Timeout == 0 {
				health.Timeout = Duration(30 * time.Second)
			}
			if health.Interval == 0 {
				health.Interval = Duration(500 * time.Millisecond)
			}
			restart := mergeRestart(defaults.Restart, process.Restart)
			if restart.Policy == "" {
				restart.Policy = "never"
			}
			stopTimeout := process.StopTimeout
			if stopTimeout == 0 {
				stopTimeout = defaults.StopTimeout
			}
			return ResolvedProcess{
				ProjectName: project.Name,
				Name:        process.Name,
				Directory:   filepath.Clean(directory),
				Command:     process.Command,
				Args:        append([]string(nil), process.Args...),
				Shell:       process.Shell,
				Env:         mergeEnv(os.Environ(), fileEnv, process.Env),
				DependsOn:   append([]string(nil), process.DependsOn...),
				Autostart:   process.Autostart,
				StopTimeout: time.Duration(stopTimeout),
				Health:      health,
				Restart:     restart,
				Log:         mergeLog(defaults.Log, process.Log),
			}, nil
		}
		return ResolvedProcess{}, fmt.Errorf("project %q has no process %q", projectName, processName)
	}
	return ResolvedProcess{}, fmt.Errorf("project %q not found", projectName)
}

func effectiveDefaults(got Defaults) Defaults {
	defaults := Default().Defaults
	if got.StopTimeout != 0 {
		defaults.StopTimeout = got.StopTimeout
	}
	defaults.Log = mergeLog(defaults.Log, got.Log)
	defaults.Restart = mergeRestart(defaults.Restart, got.Restart)
	return defaults
}

func mergeLog(defaults, override LogConfig) LogConfig {
	if override.MaxSizeMB != 0 {
		defaults.MaxSizeMB = override.MaxSizeMB
	}
	if override.MaxFiles != 0 {
		defaults.MaxFiles = override.MaxFiles
	}
	if override.BufferLines != 0 {
		defaults.BufferLines = override.BufferLines
	}
	return defaults
}

func mergeRestart(defaults, override RestartConfig) RestartConfig {
	if override.Policy != "" {
		defaults.Policy = override.Policy
	}
	if override.MaxAttempts != 0 {
		defaults.MaxAttempts = override.MaxAttempts
	}
	if override.Window != 0 {
		defaults.Window = override.Window
	}
	if override.InitialBackoff != 0 {
		defaults.InitialBackoff = override.InitialBackoff
	}
	if override.MaxBackoff != 0 {
		defaults.MaxBackoff = override.MaxBackoff
	}
	return defaults
}

func mergeEnv(parent []string, file, explicit map[string]string) []string {
	values := make(map[string]string, len(parent)+len(file)+len(explicit))
	for _, item := range parent {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[key] = value
		}
	}
	for key, value := range file {
		values[key] = value
	}
	for key, value := range explicit {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}

func requireDirectory(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%q is not a directory", path)
	}
	return nil
}
