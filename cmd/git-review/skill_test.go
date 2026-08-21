package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-review/internal/skill"
)

func TestInstallSkillDefinition(t *testing.T) {
	tempDir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error: %v", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Chdir() error: %v", err)
	}
	defer func() {
		_ = os.Chdir(origWd)
	}()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error: %v", err)
	}
	os.Stdout = w

	installSkillDefinition()

	_ = w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	output := buf.String()

	expectedRel := filepath.Join(".", ".agents", "skills", "git-review", "SKILL.md")
	expectedMsg := "Installed git-review skill to " + expectedRel + "\n"
	if output != expectedMsg {
		t.Errorf("installSkillDefinition() output = %q, want %q", output, expectedMsg)
	}

	content, err := os.ReadFile(expectedRel)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if string(content) != skill.Content {
		t.Errorf("installed file content does not match skill.Content")
	}
	if !strings.Contains(string(content), "name: git-review") {
		t.Errorf("installed skill content missing frontmatter name: git-review")
	}
}
