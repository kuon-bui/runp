package config

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

const (
	initialEnvScanBuffer = 4 * 1024
	maximumEnvLineBytes  = 1 << 20
)

func ParseEnv(r io.Reader) (map[string]string, error) {
	values := make(map[string]string)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, initialEnvScanBuffer), maximumEnvLineBytes)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, raw, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || !envKeyPattern.MatchString(key) {
			return nil, fmt.Errorf("line %d: invalid environment assignment", lineNumber)
		}
		value, err := parseEnvValue(raw)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func parseEnvValue(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if raw[0] == '\'' {
		if len(raw) < 2 || raw[len(raw)-1] != '\'' {
			return "", fmt.Errorf("unterminated single-quoted value")
		}
		return raw[1 : len(raw)-1], nil
	}
	if raw[0] == '"' {
		if len(raw) < 2 || raw[len(raw)-1] != '"' {
			return "", fmt.Errorf("unterminated double-quoted value")
		}
		value, err := strconv.Unquote(raw)
		if err != nil {
			return "", fmt.Errorf("invalid double-quoted value: %w", err)
		}
		return value, nil
	}
	return raw, nil
}
