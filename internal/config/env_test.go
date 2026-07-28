package config_test

import (
	"reflect"
	"strings"
	"testing"

	"runp/internal/config"
)

func TestParseEnv(t *testing.T) {
	input := `
# comment
export A=one
B='two words'
C="three\nlines"
D=  plain value  
A=last
`
	got, err := config.ParseEnv(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"A": "last", "B": "two words", "C": "three\nlines", "D": "plain value"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestParseEnvDoesNotExpand(t *testing.T) {
	got, err := config.ParseEnv(strings.NewReader("A=$HOME\nB=\"$A value\"\nC=`whoami`\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"A": "$HOME", "B": "$A value", "C": "`whoami`"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
}

func TestParseEnvRejectsMalformedLines(t *testing.T) {
	for _, input := range []string{"NO_EQUALS\n", "1BAD=value\n", "A='unterminated\n", "A=\"unterminated\n"} {
		if _, err := config.ParseEnv(strings.NewReader(input)); err == nil {
			t.Fatalf("expected %q to fail", input)
		}
	}
}
