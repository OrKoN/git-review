package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelectPatchKeepsSelectedChangedLine(t *testing.T) {
	diff := []byte("diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1,3 +1,3 @@\n one\n-two\n+TWO\n-three\n+THREE\n")
	patch, err := SelectPatch(diff, 0, []int{0, 1})
	if err != nil {
		t.Fatal(err)
	}
	got := string(patch)
	if !strings.Contains(got, "-two\n+TWO\n") {
		t.Fatalf("selected change missing:\n%s", got)
	}
	if strings.Contains(got, "+THREE") || strings.Contains(got, "-three") {
		t.Fatalf("unselected change retained:\n%s", got)
	}
	if !strings.Contains(got, " three\n") {
		t.Fatalf("unselected deletion not converted to context:\n%s", got)
	}
}

func TestSelectPatchRejectsBadSelection(t *testing.T) {
	diff := []byte("diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1 +1 @@\n-a\n+b\n")
	for _, lines := range [][]int{{-1}, {4}, {0, 0}} {
		if _, err := SelectPatch(diff, 0, lines); err == nil {
			t.Errorf("SelectPatch accepted %v", lines)
		}
	}
}

func TestApplyPatchStagesAndDiscardsSelectedLines(t *testing.T) {
	repo, root := testRepo(t)
	content := "one\nTWO\nTHREE\n"
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	diff, fp, err := repo.DiffWithFingerprint(context.Background(), "tracked.txt", "unstaged")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(diff), "+TWO") {
		t.Fatalf("unexpected diff:\n%s", diff)
	}
	if err := repo.ApplyPatch(context.Background(), PatchTarget{Path: "tracked.txt", Action: "stage", Fingerprint: fp, Hunk: 0, Lines: []int{0, 1}}); err != nil {
		t.Fatal(err)
	}
	staged := runGit(t, root, "show", ":tracked.txt")
	if staged != "one\nTWO\n" {
		t.Fatalf("staged content = %q", staged)
	}
	worktree, err := os.ReadFile(filepath.Join(root, "tracked.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(worktree) != content {
		t.Fatalf("staging changed worktree to %q", worktree)
	}
}

func TestApplyPatchUnstagesAndDiscardsAddition(t *testing.T) {
	repo, root := testRepo(t)
	content := "one\ntwo\nTHREE\n"
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.Stage(context.Background(), "tracked.txt"); err != nil {
		t.Fatal(err)
	}
	_, fp, err := repo.DiffWithFingerprint(context.Background(), "tracked.txt", "staged")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ApplyPatch(context.Background(), PatchTarget{Path: "tracked.txt", Action: "unstage", Fingerprint: fp, Hunk: 0, Lines: []int{0}}); err != nil {
		t.Fatal(err)
	}
	if got := runGit(t, root, "show", ":tracked.txt"); got != "one\ntwo\n" {
		t.Fatalf("index after unstage = %q", got)
	}
	_, fp, err = repo.DiffWithFingerprint(context.Background(), "tracked.txt", "unstaged")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ApplyPatch(context.Background(), PatchTarget{Path: "tracked.txt", Action: "discard", Fingerprint: fp, Hunk: 0, Lines: []int{0}}); err != nil {
		t.Fatal(err)
	}
	worktree, _ := os.ReadFile(filepath.Join(root, "tracked.txt"))
	if string(worktree) != "one\ntwo\n" {
		t.Fatalf("worktree after discard = %q", worktree)
	}
}

func TestApplyPatchPartiallyStagesUntrackedFile(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("first\nsecond\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	_, fp, err := repo.DiffWithFingerprint(context.Background(), "new.txt", "unstaged")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ApplyPatch(context.Background(), PatchTarget{Path: "new.txt", Action: "stage", Fingerprint: fp, Hunk: 0, Lines: []int{0}}); err != nil {
		t.Fatal(err)
	}
	if got := runGit(t, root, "show", ":new.txt"); got != "first\n" {
		t.Fatalf("partially staged content = %q", got)
	}
	worktree, _ := os.ReadFile(filepath.Join(root, "new.txt"))
	if string(worktree) != "first\nsecond\n" {
		t.Fatalf("worktree changed to %q", worktree)
	}
}
