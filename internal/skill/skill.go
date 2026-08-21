package skill

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

// Content contains the Markdown definition for the git-review agent skill.
//
//go:embed SKILL.md
var Content string

// DefaultProjectPath returns the default target path for the agent skill in a project:
// <root>/.agents/skills/git-review/SKILL.md
func DefaultProjectPath(root string) string {
	if root == "" {
		root = "."
	}
	return filepath.Join(root, ".agents", "skills", "git-review", "SKILL.md")
}

// Install writes the git-review skill definition to the target file path.
// If targetPath is empty, DefaultProjectPath(".") is used.
func Install(targetPath string) (string, error) {
	if targetPath == "" {
		targetPath = DefaultProjectPath(".")
	}
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create skill directory %s: %w", dir, err)
	}
	if err := os.WriteFile(targetPath, []byte(Content), 0o644); err != nil {
		return "", fmt.Errorf("write skill file %s: %w", targetPath, err)
	}
	return targetPath, nil
}

// InstallInProject writes the skill definition to <root>/.agents/skills/git-review/SKILL.md.
func InstallInProject(root string) (string, error) {
	return Install(DefaultProjectPath(root))
}
