package repository

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStageUnstageAndDiscard(t *testing.T) {
	repo, root := testRepo(t)
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.Stage(context.Background(), "tracked.txt"); err != nil {
		t.Fatal(err)
	}
	if got := runGit(t, root, "diff", "--cached", "--name-only"); got != "tracked.txt\n" {
		t.Fatalf("staged names = %q", got)
	}
	if err := repo.Unstage(context.Background(), "tracked.txt"); err != nil {
		t.Fatal(err)
	}
	if got := runGit(t, root, "diff", "--cached", "--name-only"); got != "" {
		t.Fatalf("still staged: %q", got)
	}
	if err := repo.Discard(context.Background(), "tracked.txt"); err != nil {
		t.Fatal(err)
	}
	if got := runGit(t, root, "status", "--porcelain"); got != "" {
		t.Fatalf("repository still dirty: %q", got)
	}
}

func TestUntrackedDiffAndUnbornUnstage(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	diff, err := repo.Diff(context.Background(), "new.txt", "unstaged")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(diff), "+hello") {
		t.Fatalf("untracked diff missing content:\n%s", diff)
	}
	if err := repo.Stage(context.Background(), "new.txt"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Unstage(context.Background(), "new.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); err != nil {
		t.Fatalf("unstage removed worktree file: %v", err)
	}
}

func TestCommit(t *testing.T) {
	repo, root := testRepo(t)
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.Stage(context.Background(), "tracked.txt"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Commit(context.Background(), "Update tracked file", "body"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(runGit(t, root, "log", "-1", "--pretty=%s")); got != "Update tracked file" {
		t.Fatalf("subject = %q", got)
	}
}

func TestCommitRejectsNoStagedChanges(t *testing.T) {
	repo, _ := testRepo(t)
	if err := repo.Commit(context.Background(), "Nothing", ""); !errors.Is(err, ErrNoStagedChanges) {
		t.Fatalf("Commit error = %v", err)
	}
}

func TestDiffFingerprintTracksContentWhenDiffTextDoesNot(t *testing.T) {
	repo, root := testRepo(t)
	path := filepath.Join(root, "binary.dat")
	if err := os.WriteFile(path, []byte{'a', 0, 'b'}, 0o644); err != nil {
		t.Fatal(err)
	}
	_, first, err := repo.DiffWithFingerprint(context.Background(), "binary.dat", "unstaged")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte{'c', 0, 'd'}, 0o644); err != nil {
		t.Fatal(err)
	}
	_, second, err := repo.DiffWithFingerprint(context.Background(), "binary.dat", "unstaged")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("binary content change did not change diff fingerprint")
	}
}

func TestDiffRejectsOversizedText(t *testing.T) {
	repo, root := testRepo(t)
	large := make([]byte, MaxEditableSize+1)
	for i := range large {
		large[i] = 'x'
	}
	if err := os.WriteFile(filepath.Join(root, "large.txt"), large, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Diff(context.Background(), "large.txt", "unstaged"); err == nil {
		t.Fatal("Diff accepted oversized text")
	}
}

func TestDiffRejectsOversizedStagedDeletion(t *testing.T) {
	repo, root := testRepo(t)
	large := make([]byte, MaxEditableSize+1)
	for i := range large {
		large[i] = 'x'
	}
	path := filepath.Join(root, "large.txt")
	if err := os.WriteFile(path, large, 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "large.txt")
	runGit(t, root, "commit", "-qm", "large")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "large.txt")
	if _, err := repo.Diff(context.Background(), "large.txt", "staged"); err == nil {
		t.Fatal("Diff accepted oversized staged deletion")
	}
}

func TestUnusualFilenameUsesLiteralGitPath(t *testing.T) {
	repo, root := testRepo(t)
	path := "- spaced ünicode.txt"
	if err := os.WriteFile(filepath.Join(root, path), []byte("content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.Stage(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	if got := runGit(t, root, "diff", "--cached", "--name-only", "-z"); got != path+"\x00" {
		t.Fatalf("staged path = %q", got)
	}
	if err := repo.Unstage(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	if err := repo.Discard(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
		t.Fatalf("untracked file still exists: %v", err)
	}
}

func TestConflictAllowsEditAndWholeFileResolutionOnly(t *testing.T) {
	repo, root := testRepo(t)
	baseBranch := strings.TrimSpace(runGit(t, root, "branch", "--show-current"))
	runGit(t, root, "checkout", "-qb", "side")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "commit", "-qam", "side")
	runGit(t, root, "checkout", "-q", baseBranch)
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "commit", "-qam", "main")
	cmd := exec.Command("git", "-C", root, "merge", "side")
	if err := cmd.Run(); err == nil {
		t.Fatal("merge unexpectedly succeeded")
	}
	state, err := repo.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Files) != 1 || !state.Files[0].Conflicted {
		t.Fatalf("conflict state = %+v", state.Files)
	}
	content, err := repo.ReadFile("tracked.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.WriteFile("tracked.txt", content.Fingerprint, "resolved\n"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ApplyPatch(context.Background(), PatchTarget{Path: "tracked.txt", Action: "stage", Fingerprint: "invalid", Hunk: 0}); err == nil || !strings.Contains(err.Error(), "conflicted") {
		t.Fatalf("granular conflict action error = %v", err)
	}
	if err := repo.Stage(context.Background(), "tracked.txt"); err != nil {
		t.Fatal(err)
	}
	state, err = repo.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Files) != 1 || state.Files[0].Conflicted {
		t.Fatalf("resolution state = %+v", state.Files)
	}
}
