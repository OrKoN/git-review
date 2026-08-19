package repository

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

type PatchTarget struct {
	Path        string
	Area        string
	Action      string
	Fingerprint string
	Hunk        int
	Lines       []int
}

// SelectPatch keeps one hunk and, when lines is non-empty, only the selected
// changed lines. Line numbers are zero-based among changed lines in that hunk.
func SelectPatch(diff []byte, wantedHunk int, lines []int) ([]byte, error) {
	all := strings.SplitAfter(string(diff), "\n")
	headerEnd := -1
	var starts []int
	for i, line := range all {
		if strings.HasPrefix(line, "@@ ") {
			starts = append(starts, i)
			if headerEnd < 0 {
				headerEnd = i
			}
		}
	}
	if headerEnd < 0 || wantedHunk < 0 || wantedHunk >= len(starts) {
		return nil, errors.New("hunk not found")
	}
	end := len(all)
	if wantedHunk+1 < len(starts) {
		end = starts[wantedHunk+1]
	}
	selected := map[int]bool{}
	for _, line := range lines {
		if line < 0 {
			return nil, errors.New("invalid line selection")
		}
		selected[line] = true
	}
	if len(selected) != len(lines) {
		return nil, errors.New("duplicate line selection")
	}

	result := append([]string{}, all[:headerEnd]...)
	hunk := all[starts[wantedHunk]:end]
	result = append(result, hunk[0])
	changed := 0
	kept := 0
	for _, line := range hunk[1:] {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "\\ No newline") {
			result = append(result, line)
			continue
		}
		if line[0] != '+' && line[0] != '-' {
			result = append(result, line)
			continue
		}
		if len(lines) == 0 || selected[changed] {
			result = append(result, line)
			kept++
		} else if line[0] == '-' {
			result = append(result, " "+line[1:])
		}
		changed++
	}
	if len(lines) > 0 && kept != len(lines) {
		return nil, errors.New("selected line not found")
	}
	return []byte(strings.Join(result, "")), nil
}

func (r *Repository) ApplyPatch(ctx context.Context, target PatchTarget) error {
	if _, err := r.cleanPath(target.Path); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state, err := r.State(ctx)
	if err != nil {
		return err
	}
	for _, file := range state.Files {
		if file.Path == target.Path && file.Conflicted {
			return errors.New("granular patch actions are disabled for conflicted files")
		}
	}
	if target.Action != "stage" && target.Action != "unstage" && target.Action != "discard" {
		return errors.New("invalid patch action")
	}
	area := "unstaged"
	if target.Action == "unstage" {
		area = "staged"
	}
	diff, currentFingerprint, err := r.DiffWithFingerprint(ctx, target.Path, area)
	if err != nil {
		return err
	}
	if currentFingerprint != target.Fingerprint {
		return ErrStale
	}
	if len(target.Lines) > 1 {
		slices.Sort(target.Lines)
		for i := 1; i < len(target.Lines); i++ {
			if target.Lines[i] != target.Lines[i-1]+1 {
				return errors.New("selected lines must be contiguous")
			}
		}
	}
	patch, err := SelectPatch(diff, target.Hunk, target.Lines)
	if err != nil {
		return err
	}

	args := []string{"apply", "--recount", "--whitespace=nowarn"}
	switch target.Action {
	case "stage":
		args = append(args, "--cached")
	case "unstage":
		args = append(args, "--cached", "--reverse")
	case "discard":
		args = append(args, "--reverse")
	}
	if _, err := r.git(ctx, patch, args...); err != nil {
		return fmt.Errorf("apply selected patch: %w", err)
	}
	return nil
}
