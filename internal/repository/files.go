package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (r *Repository) Files(ctx context.Context) ([]File, error) {
	args := []string{"ls-files", "-z", "--cached", "--others", "--exclude-standard"}
	if r.fileScope != "" {
		args = append(args, "--", ":(top,literal)"+r.fileScope)
	}
	out, err := r.git(ctx, nil, args...)
	if err != nil {
		return nil, err
	}
	files := make([]File, 0)
	seen := make(map[string]struct{})
	for _, raw := range bytes.Split(out, []byte{0}) {
		path := string(raw)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		kind, size := r.fileMetadata(path)
		files = append(files, File{Path: path, Kind: kind, Size: size})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func (r *Repository) IsReviewable(ctx context.Context, path string) bool {
	if _, err := r.cleanPath(path); err != nil {
		return false
	}
	files, err := r.Files(ctx)
	if err != nil {
		return false
	}
	for _, file := range files {
		if file.Path == path {
			return file.Kind == "file"
		}
	}
	return false
}

func (r *Repository) fileMetadata(path string) (string, int64) {
	info, err := os.Lstat(filepath.Join(r.root, filepath.FromSlash(path)))
	if err != nil {
		return "missing", 0
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return "symlink", info.Size()
	case info.IsDir():
		return "directory", 0
	case info.Mode().IsRegular():
		return "file", info.Size()
	default:
		return "other", info.Size()
	}
}

func (r *Repository) IsChanged(ctx context.Context, path string) bool {
	if _, err := r.cleanPath(path); err != nil {
		return false
	}
	state, err := r.State(ctx)
	if err != nil {
		return false
	}
	for _, file := range state.Files {
		if file.Path == path {
			return true
		}
	}
	return false
}

func (r *Repository) SnapshotFingerprint(ctx context.Context) (string, error) {
	out, err := r.git(ctx, nil, "status", "--porcelain=v2", "-z", "--branch", "--untracked-files=all")
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, _ = hash.Write(out)
	parts := bytes.Split(out, []byte{0})
	for i := 0; i < len(parts); i++ {
		entry := string(parts[i])
		if entry == "" || entry[0] == '#' {
			continue
		}
		var path string
		switch entry[0] {
		case '1':
			fields := strings.SplitN(entry, " ", 9)
			if len(fields) == 9 {
				path = fields[8]
			}
		case '2':
			fields := strings.SplitN(entry, " ", 10)
			if len(fields) == 10 {
				path = fields[9]
			}
			if i+1 < len(parts) {
				i++
			}
		case 'u':
			fields := strings.SplitN(entry, " ", 11)
			if len(fields) == 11 {
				path = fields[10]
			}
		case '?':
			if len(entry) >= 3 {
				path = entry[2:]
			}
		}
		if path != "" {
			abs, cleanErr := r.cleanPath(path)
			if cleanErr == nil {
				info, statErr := os.Lstat(abs)
				if statErr == nil {
					fmt.Fprintf(hash, "|%s:%d:%d:%d", path, info.Size(), info.ModTime().UnixNano(), info.Mode())
				}
			}
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (r *Repository) cleanPath(path string) (string, error) {
	if path == "" || strings.ContainsRune(path, 0) || filepath.IsAbs(path) {
		return "", errors.New("invalid repository path")
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes repository")
	}
	abs := filepath.Join(r.root, clean)
	rel, err := filepath.Rel(r.root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes repository")
	}
	resolvedRoot, rootErr := filepath.EvalSymlinks(r.root)
	resolvedParent, parentErr := filepath.EvalSymlinks(filepath.Dir(abs))
	if rootErr != nil || parentErr != nil {
		return "", errors.New("cannot resolve repository path")
	}
	parentRel, relErr := filepath.Rel(resolvedRoot, resolvedParent)
	if relErr != nil || parentRel == ".." || strings.HasPrefix(parentRel, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes repository through a symlink")
	}
	return abs, nil
}

func fingerprint(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (r *Repository) ReadFile(path string) (Content, error) {
	if !r.IsReviewable(context.Background(), path) {
		return Content{}, errors.New("file is not a reviewable repository file")
	}
	return r.readFile(path)
}

func (r *Repository) readFile(path string) (Content, error) {
	abs, err := r.cleanPath(path)
	if err != nil {
		return Content{}, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return Content{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxEditableSize {
		return Content{}, errors.New("file is not editable")
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return Content{}, err
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return Content{}, errors.New("binary file is not editable")
	}
	return Content{Path: path, Text: string(data), Fingerprint: fingerprint(data)}, nil
}

func (r *Repository) WriteFile(path, expected, text string) (Content, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.IsChanged(context.Background(), path) {
		return Content{}, errors.New("file is not changed")
	}
	current, err := r.readFile(path)
	if err != nil {
		return Content{}, err
	}
	if current.Fingerprint != expected {
		return Content{}, ErrStale
	}
	abs, _ := r.cleanPath(path)
	if err := os.WriteFile(abs, []byte(text), 0o600); err != nil {
		return Content{}, err
	}
	return r.readFile(path)
}
