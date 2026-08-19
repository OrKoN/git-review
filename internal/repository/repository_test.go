package repository

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGitQueueHonorsCancellation(t *testing.T) {
	repo, _ := testRepo(t)
	repo.gitQueue <- struct{}{}
	defer func() { <-repo.gitQueue }()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := repo.State(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("State error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("canceled queue wait took %v", elapsed)
	}
}

func TestGitQueueWaitsForPreviousOperation(t *testing.T) {
	repo, _ := testRepo(t)
	repo.gitQueue <- struct{}{}
	done := make(chan error, 1)
	go func() {
		_, err := repo.State(context.Background())
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("queued State returned early: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	<-repo.gitQueue

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("queued State failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued State did not run after the queue was released")
	}
}

func testRepo(t *testing.T) (*Repository, string) {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	runGit(t, root, "config", "user.name", "Test User")
	runGit(t, root, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "commit", "-qm", "initial")
	repo, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return repo, root
}

func runGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func TestOpenRejectsNonRepository(t *testing.T) {
	if _, err := Open(t.TempDir()); err == nil {
		t.Fatal("Open succeeded outside a repository")
	}
}

func TestParseStatusV2(t *testing.T) {
	input := []byte("# branch.head main\x001 .M N... 100644 100644 100644 abc def path with space.txt\x002 R. N... 100644 100644 100644 abc def R100 renamed.txt\x00old.txt\x00u UU N... 100644 100644 100644 100644 a b c conflict.txt\x00? new.txt\x00")
	state, err := parseStatusV2(input)
	if err != nil {
		t.Fatal(err)
	}
	if state.Branch != "main" || len(state.Files) != 4 {
		t.Fatalf("state = %+v", state)
	}
	if state.Files[0].Path != "path with space.txt" || state.Files[0].Index != " " || state.Files[0].Worktree != "M" {
		t.Fatalf("ordinary = %+v", state.Files[0])
	}
	if state.Files[1].Path != "renamed.txt" || state.Files[1].Index != "R" || state.Files[1].Worktree != " " {
		t.Fatalf("rename = %+v", state.Files[1])
	}
	if !state.Files[2].Conflicted || state.Files[2].Path != "conflict.txt" {
		t.Fatalf("conflict = %+v", state.Files[2])
	}
	if !state.Files[3].Untracked || state.Files[3].Path != "new.txt" {
		t.Fatalf("untracked = %+v", state.Files[3])
	}
}

func TestReadFileRejectsEscapesAndBinary(t *testing.T) {
	repo, root := testRepo(t)
	if _, err := repo.ReadFile("../secret"); err == nil {
		t.Fatal("ReadFile accepted traversal")
	}
	if _, err := repo.ReadFile("tracked.txt"); err != nil {
		t.Fatalf("ReadFile rejected a clean tracked file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "binary.dat"), []byte{'a', 0, 'b'}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ReadFile("binary.dat"); err == nil {
		t.Fatal("ReadFile accepted binary content")
	}
}

func TestFilesListsTrackedAndNonIgnoredUntracked(t *testing.T) {
	repo, root := testRepo(t)
	for name, text := range map[string]string{"visible.txt": "visible\n", ".gitignore": "ignored.txt\n", "ignored.txt": "ignored\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := repo.Files(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, len(files))
	for index, file := range files {
		paths[index] = file.Path
	}
	if strings.Join(paths, ",") != ".gitignore,tracked.txt,visible.txt" {
		t.Fatalf("paths = %q", paths)
	}
}

func TestFilesOnlyListsInvocationDirectory(t *testing.T) {
	_, root := testRepo(t)
	for _, path := range []string{"nested/inside.txt", "nested/deeper/inside.txt", "sibling/outside.txt"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, path), []byte(path), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	repo, err := Open(filepath.Join(root, "nested"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := repo.Files(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, len(files))
	for index, file := range files {
		paths[index] = file.Path
	}
	if strings.Join(paths, ",") != "nested/deeper/inside.txt,nested/inside.txt" {
		t.Fatalf("paths = %q", paths)
	}
}

func TestStageAllValidatesRepositoryFingerprint(t *testing.T) {
	repo, root := testRepo(t)
	fingerprint, err := repo.SnapshotFingerprint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.StageAll(context.Background(), fingerprint); !errors.Is(err, ErrStale) {
		t.Fatalf("StageAll stale error = %v", err)
	}
	fingerprint, _ = repo.SnapshotFingerprint(context.Background())
	if err := repo.StageAll(context.Background(), fingerprint); err != nil {
		t.Fatal(err)
	}
	state, _ := repo.State(context.Background())
	if len(state.Files) != 1 || state.Files[0].Path != "new.txt" || state.Files[0].Index != "A" {
		t.Fatalf("state = %+v", state)
	}
}

func TestWriteFileRejectsStaleContent(t *testing.T) {
	repo, root := testRepo(t)
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("browser base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	content, err := repo.ReadFile("tracked.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("external\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.WriteFile("tracked.txt", content.Fingerprint, "browser\n"); err != ErrStale {
		t.Fatalf("got %v, want ErrStale", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "tracked.txt"))
	if string(got) != "external\n" {
		t.Fatalf("stale write changed content to %q", got)
	}
}

func TestReadFileRejectsIntermediateSymlinkEscape(t *testing.T) {
	repo, root := testRepo(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.readFile("escape/secret.txt"); err == nil {
		t.Fatal("readFile followed an intermediate symlink outside the repository")
	}
}
