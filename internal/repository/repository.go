package repository

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

const MaxEditableSize = 2 << 20

type Repository struct {
	root      string
	directory string
	fileScope string
	gitDir    string
	mu        sync.Mutex
	gitQueue  chan struct{}
}

type File struct {
	Path       string `json:"path"`
	Index      string `json:"index"`
	Worktree   string `json:"worktree"`
	Untracked  bool   `json:"untracked"`
	Conflicted bool   `json:"conflicted"`
	Kind       string `json:"kind"`
	Size       int64  `json:"size,omitempty"`
}

type State struct {
	Branch        string `json:"branch"`
	Files         []File `json:"files"`
	Fingerprint   string `json:"fingerprint,omitempty"`
	ReviewMessage string `json:"reviewMessage,omitempty"`
}

type Content struct {
	Path        string `json:"path"`
	Text        string `json:"text"`
	Fingerprint string `json:"fingerprint"`
}

func Open(dir string) (*Repository, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return nil, errors.New("current directory is not in a Git worktree")
	}
	root, err := filepath.Abs(strings.TrimSpace(string(out)))
	if err != nil {
		return nil, err
	}
	directory, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	scope, err := filepath.Rel(root, directory)
	if err != nil || scope == ".." || strings.HasPrefix(scope, ".."+string(filepath.Separator)) {
		return nil, errors.New("current directory is outside the Git worktree")
	}
	if scope == "." {
		scope = ""
	}
	gitDirOut, err := exec.Command("git", "-C", root, "rev-parse", "--absolute-git-dir").Output()
	if err != nil {
		return nil, errors.New("cannot discover Git directory")
	}
	gitDir, err := filepath.Abs(strings.TrimSpace(string(gitDirOut)))
	if err != nil {
		return nil, err
	}
	return &Repository{
		root: root, directory: directory, fileScope: filepath.ToSlash(scope), gitDir: gitDir, gitQueue: make(chan struct{}, 1),
	}, nil
}

func (r *Repository) Root() string      { return r.root }
func (r *Repository) Directory() string { return r.directory }
func (r *Repository) GitDir() string    { return r.gitDir }

func (r *Repository) WatchDirectories(ctx context.Context) []string {
	directories := map[string]struct{}{r.root: {}, r.gitDir: {}}
	state, err := r.State(ctx)
	if err != nil {
		return []string{r.root, r.gitDir}
	}
	for _, file := range state.Files {
		directory := filepath.Dir(filepath.Join(r.root, filepath.FromSlash(file.Path)))
		for {
			directories[directory] = struct{}{}
			if directory == r.root {
				break
			}
			parent := filepath.Dir(directory)
			if parent == directory || !strings.HasPrefix(parent, r.root) {
				break
			}
			directory = parent
		}
	}
	result := make([]string, 0, len(directories))
	for directory := range directories {
		result = append(result, directory)
	}
	return result
}

func (r *Repository) git(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	select {
	case r.gitQueue <- struct{}{}:
		defer func() { <-r.gitQueue }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", r.root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && len(args) >= 2 && args[0] == "diff" && args[1] == "--no-index" {
			return stdout.Bytes(), nil
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(stdout.String())
		}
		if message == "" {
			message = err.Error()
		}
		return nil, errors.New(message)
	}
	return stdout.Bytes(), nil
}

func (r *Repository) State(ctx context.Context) (State, error) {
	out, err := r.git(ctx, nil, "status", "--porcelain=v2", "-z", "--branch", "--untracked-files=all")
	if err != nil {
		return State{}, err
	}
	state, err := parseStatusV2(out)
	if err != nil {
		return State{}, err
	}
	for i := range state.Files {
		state.Files[i].Kind, state.Files[i].Size = r.fileMetadata(state.Files[i].Path)
	}
	return state, nil
}

func parseStatusV2(out []byte) (State, error) {
	parts := bytes.Split(out, []byte{0})
	state := State{Files: make([]File, 0)}
	for i := 0; i < len(parts); i++ {
		entry := string(parts[i])
		if entry == "" {
			continue
		}
		if strings.HasPrefix(entry, "# branch.head ") {
			state.Branch = strings.TrimPrefix(entry, "# branch.head ")
			continue
		}
		var path, index, worktree string
		switch entry[0] {
		case '1':
			fields := strings.SplitN(entry, " ", 9)
			if len(fields) != 9 || len(fields[1]) != 2 {
				return State{}, errors.New("invalid ordinary status entry")
			}
			index, worktree, path = fields[1][0:1], fields[1][1:2], fields[8]
		case '2':
			fields := strings.SplitN(entry, " ", 10)
			if len(fields) != 10 || len(fields[1]) != 2 {
				return State{}, errors.New("invalid rename status entry")
			}
			index, worktree, path = fields[1][0:1], fields[1][1:2], fields[9]
			if i+1 < len(parts) {
				i++
			}
		case 'u':
			fields := strings.SplitN(entry, " ", 11)
			if len(fields) != 11 || len(fields[1]) != 2 {
				return State{}, errors.New("invalid unmerged status entry")
			}
			index, worktree, path = fields[1][0:1], fields[1][1:2], fields[10]
		case '?':
			if len(entry) < 3 {
				return State{}, errors.New("invalid untracked status entry")
			}
			index, worktree, path = " ", "?", entry[2:]
		default:
			continue
		}
		if index == "." {
			index = " "
		}
		if worktree == "." {
			worktree = " "
		}
		conflicted := index == "U" || worktree == "U" || index+worktree == "AA" || index+worktree == "DD"
		state.Files = append(state.Files, File{Path: path, Index: index, Worktree: worktree, Untracked: worktree == "?", Conflicted: conflicted})
	}
	return state, nil
}

// Files returns tracked and non-ignored untracked files within the directory
// passed to Open. Content is not read so callers can browse large trees lazily.
