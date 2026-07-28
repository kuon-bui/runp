package logstore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRotationKeepsConfiguredTotal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.log")
	w, err := newRotatingWriter(path, 8, 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"1111\n", "2222\n", "3333\n", "4444\n"} {
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "api.log*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 3 {
		t.Fatalf("files = %v", matches)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	archive1, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	archive2, err := os.ReadFile(path + ".2")
	if err != nil {
		t.Fatal(err)
	}
	if string(archive2) != "2222\n" || string(archive1) != "3333\n" || string(current) != "4444\n" {
		t.Fatalf("archives = %q %q %q", archive2, archive1, current)
	}
}

func TestRotationWithOneFileKeepsCurrentOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api.log")
	w, err := newRotatingWriter(path, 5, 1)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("one\n"))
	_, _ = w.Write([]byte("two\n"))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(path + "*")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("files = %v", matches)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "two\n" {
		t.Fatalf("current = %q", got)
	}
}
