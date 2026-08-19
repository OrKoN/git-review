package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

var ErrStale = errors.New("repository state changed")
var ErrNoStagedChanges = errors.New("no staged changes to commit")

func (r *Repository) Diff(ctx context.Context, path, area string) ([]byte, error) {
	abs, err := r.cleanPath(path)
	if err != nil {
		return nil, err
	}
	if info, statErr := os.Lstat(abs); statErr == nil && info.Mode().IsRegular() && info.Size() > MaxEditableSize {
		return nil, errors.New("file is too large to diff")
	}
	args := []string{"diff", "--no-color", "--no-ext-diff", "--no-textconv", "--unified=3"}
	if area == "staged" {
		if r.objectTooLarge(ctx, ":"+path) || r.objectTooLarge(ctx, "HEAD:"+path) {
			return nil, errors.New("file is too large to diff")
		}
		args = append(args, "--cached")
	} else if area != "unstaged" {
		return nil, errors.New("area must be staged or unstaged")
	} else if r.objectTooLarge(ctx, ":"+path) {
		return nil, errors.New("file is too large to diff")
	}
	args = append(args, "--", path)
	out, err := r.git(ctx, nil, args...)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 && area == "unstaged" {
		// Git diff omits untracked files; present them as additions.
		state, stateErr := r.State(ctx)
		if stateErr == nil {
			for _, file := range state.Files {
				if file.Path == path && file.Untracked {
					return r.git(ctx, nil, "diff", "--no-index", "--", "/dev/null", path)
				}
			}
		}
	}
	return out, nil
}

func (r *Repository) objectTooLarge(ctx context.Context, object string) bool {
	sizeOut, err := r.git(ctx, nil, "cat-file", "-s", object)
	if err != nil {
		return false
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(sizeOut)), 10, 64)
	return err == nil && size > MaxEditableSize
}

func (r *Repository) DiffWithFingerprint(ctx context.Context, path, area string) ([]byte, string, error) {
	diff, err := r.Diff(ctx, path, area)
	if err != nil {
		return nil, "", err
	}
	hash := sha256.New()
	_, _ = hash.Write(diff)
	if area == "staged" {
		object, objectErr := r.git(ctx, nil, "rev-parse", ":"+path)
		if objectErr == nil {
			_, _ = hash.Write(object)
		}
	} else {
		abs, pathErr := r.cleanPath(path)
		if pathErr == nil {
			info, statErr := os.Lstat(abs)
			if statErr == nil && info.Mode().IsRegular() {
				file, openErr := os.Open(abs)
				if openErr == nil {
					_, _ = io.Copy(hash, file)
					_ = file.Close()
				}
			} else if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
				link, _ := os.Readlink(abs)
				_, _ = hash.Write([]byte(link))
			}
		}
	}
	return diff, hex.EncodeToString(hash.Sum(nil)), nil
}

func (r *Repository) Stage(ctx context.Context, path string) error {
	if _, err := r.cleanPath(path); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, err := r.git(ctx, nil, "add", "--", path)
	return err
}

func (r *Repository) StageAll(ctx context.Context, expected string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, err := r.SnapshotFingerprint(ctx)
	if err != nil {
		return err
	}
	if expected == "" || current != expected {
		return ErrStale
	}
	_, err = r.git(ctx, nil, "add", "-A", "--", ".")
	return err
}

func (r *Repository) Unstage(ctx context.Context, path string) error {
	if _, err := r.cleanPath(path); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, headErr := r.git(ctx, nil, "rev-parse", "--verify", "HEAD")
	if headErr != nil {
		_, err := r.git(ctx, nil, "rm", "--cached", "--", path)
		return err
	}
	_, err := r.git(ctx, nil, "restore", "--staged", "--", path)
	return err
}

func (r *Repository) Discard(ctx context.Context, path string) error {
	abs, err := r.cleanPath(path)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state, err := r.State(ctx)
	if err != nil {
		return err
	}
	for _, file := range state.Files {
		if file.Path != path {
			continue
		}
		if file.Untracked {
			info, statErr := os.Lstat(abs)
			if statErr != nil {
				return statErr
			}
			if info.IsDir() {
				return errors.New("refusing to delete an untracked directory")
			}
			return os.Remove(abs)
		}
		_, err = r.git(ctx, nil, "restore", "--worktree", "--", path)
		return err
	}
	return fmt.Errorf("path %q is not changed", path)
}

func (r *Repository) Commit(ctx context.Context, subject, body string) error {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return errors.New("commit subject is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state, err := r.State(ctx)
	if err != nil {
		return err
	}
	hasStaged := false
	for _, file := range state.Files {
		if file.Index != " " && file.Index != "?" {
			hasStaged = true
			break
		}
	}
	if !hasStaged {
		return ErrNoStagedChanges
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	args := []string{"commit", "-m", subject}
	if strings.TrimSpace(body) != "" {
		args = append(args, "-m", body)
	}
	_, err = r.git(ctx, nil, args...)
	return err
}
