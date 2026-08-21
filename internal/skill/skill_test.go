package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContentHasValidFrontmatter(t *testing.T) {
	if !strings.HasPrefix(Content, "---\n") {
		t.Fatalf("SKILL.md does not start with frontmatter delimiter ---")
	}
	parts := strings.SplitN(Content, "---\n", 3)
	if len(parts) < 3 {
		t.Fatalf("SKILL.md does not have closing frontmatter delimiter ---")
	}
	frontmatter := parts[1]
	if !strings.Contains(frontmatter, "name: git-review") {
		t.Errorf("frontmatter missing name: git-review\ngot:\n%s", frontmatter)
	}
	if !strings.Contains(frontmatter, "description:") {
		t.Errorf("frontmatter missing description:\ngot:\n%s", frontmatter)
	}
}

func TestDefaultProjectPath(t *testing.T) {
	path := DefaultProjectPath("/path/to/repo")
	expected := filepath.Join("/path/to/repo", ".agents", "skills", "git-review", "SKILL.md")
	if path != expected {
		t.Errorf("DefaultProjectPath(/path/to/repo) = %q, want %q", path, expected)
	}

	defaultEmpty := DefaultProjectPath("")
	expectedEmpty := filepath.Join(".", ".agents", "skills", "git-review", "SKILL.md")
	if defaultEmpty != expectedEmpty {
		t.Errorf("DefaultProjectPath(\"\") = %q, want %q", defaultEmpty, expectedEmpty)
	}
}

func TestInstall(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "custom", "skills", "git-review", "SKILL.md")

	writtenPath, err := Install(targetPath)
	if err != nil {
		t.Fatalf("Install() error: %v", err)
	}
	if writtenPath != targetPath {
		t.Fatalf("Install() = %q, want %q", writtenPath, targetPath)
	}

	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if string(data) != Content {
		t.Errorf("installed content mismatch\ngot:\n%s\nwant:\n%s", string(data), Content)
	}

	// Test overwrite
	if _, err := Install(targetPath); err != nil {
		t.Fatalf("Install() overwrite error: %v", err)
	}
}

func TestInstallInProject(t *testing.T) {
	tmpDir := t.TempDir()
	writtenPath, err := InstallInProject(tmpDir)
	if err != nil {
		t.Fatalf("InstallInProject() error: %v", err)
	}
	expected := filepath.Join(tmpDir, ".agents", "skills", "git-review", "SKILL.md")
	if writtenPath != expected {
		t.Fatalf("InstallInProject() = %q, want %q", writtenPath, expected)
	}
	data, err := os.ReadFile(expected)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if string(data) != Content {
		t.Errorf("installed content mismatch")
	}
}
