package config

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/spf13/viper"
)

var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

const (
	initialEnvScanBuffer = 4 * 1024
	maximumEnvLineBytes  = 1 << 20
)

func ParseEnv(r io.Reader) (map[string]string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	keyNames := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, initialEnvScanBuffer), maximumEnvLineBytes)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if after, ok := strings.CutPrefix(line, "export "); ok {
			line = strings.TrimSpace(after)
		}
		key, _, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || !envKeyPattern.MatchString(key) {
			return nil, fmt.Errorf("line %d: invalid environment assignment", lineNumber)
		}
		keyNames[strings.ToLower(key)] = key
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	dollarPlaceholder := "__RUNP_DOLLAR__"
	for bytes.Contains(data, []byte(dollarPlaceholder)) {
		dollarPlaceholder += "_"
	}
	data = bytes.ReplaceAll(data, []byte("$"), []byte(dollarPlaceholder))

	v := viper.New()
	v.SetConfigType("env")
	if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
		return nil, err
	}

	values := make(map[string]string, len(v.AllKeys()))
	for _, key := range v.AllKeys() {
		values[keyNames[key]] = strings.ReplaceAll(v.GetString(key), dollarPlaceholder, "$")
	}
	return values, nil
}
