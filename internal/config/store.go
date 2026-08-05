package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

const (
	configDirectoryMode = 0o700
	configFileMode      = 0o600
	localConfigName     = ".runp.json"
)

func CurrentPath() (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	return filepath.Join(directory, localConfigName), nil
}

func DataDir() (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache directory: %w", err)
	}
	return filepath.Join(root, "runp"), nil
}

func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}

	if err != nil {
		return Config{}, fmt.Errorf("open config: %w", err)
	}
	defer file.Close()

	err = file.Chmod(configFileMode)
	if err != nil {
		return Config{}, fmt.Errorf("secure config: %w", err)
	}

	data, err := io.ReadAll(file)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	v := viper.New()
	v.SetConfigType("json")
	if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	var cfg Config
	if err := v.UnmarshalExact(&cfg, viper.DecodeHook(decodeConfigValue), strictConfigDecode); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	// ponytail: Viper lowercases nested map keys; remove when it supports case-sensitive map keys.
	var original struct {
		Projects []struct {
			Processes []struct {
				Env map[string]string `json:"env"`
			} `json:"processes"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(data, &original); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	for projectIndex := range cfg.Projects {
		for processIndex := range cfg.Projects[projectIndex].Processes {
			cfg.Projects[projectIndex].Processes[processIndex].Env = original.Projects[projectIndex].Processes[processIndex].Env
		}
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate config: %w", err)
	}
	return cfg, nil
}

func strictConfigDecode(config *mapstructure.DecoderConfig) {
	config.WeaklyTypedInput = false
}

func decodeConfigValue(from, to reflect.Type, data any) (any, error) {
	if to != reflect.TypeFor[Duration]() {
		return data, nil
	}
	value, ok := data.(string)
	if !ok {
		return nil, fmt.Errorf("duration must be a string")
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return nil, fmt.Errorf("invalid duration %q: %w", value, err)
	}
	if parsed < 0 {
		return nil, fmt.Errorf("duration must not be negative")
	}
	return Duration(parsed), nil
}

func Save(path string, cfg Config) (err error) {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, configDirectoryMode); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(configFileMode); err != nil {
		return fmt.Errorf("secure temporary config: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary config: %w", err)
	}
	if err := replaceFile(temporaryPath, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	if err := syncDirectory(directory); err != nil {
		return fmt.Errorf("sync config directory: %w", err)
	}
	return nil
}
