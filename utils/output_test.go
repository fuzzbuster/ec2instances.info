package utils

import (
	"path/filepath"
	"testing"
)

func TestOutputPath(t *testing.T) {
	root, err := SetOutputDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	got, err := OutputPath(filepath.Join("azure", "instances.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "azure", "instances.json")
	if got != want {
		t.Fatalf("OutputPath() = %q, want %q", got, want)
	}
}

func TestOutputPathRejectsEscape(t *testing.T) {
	if _, err := SetOutputDir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", "..", filepath.Join("..", "outside"), filepath.Clean("/tmp/outside")} {
		if _, err := OutputPath(path); err == nil {
			t.Errorf("OutputPath(%q) returned no error", path)
		}
	}
}
