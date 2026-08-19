import { batch, computed, signal, type ReadonlySignal, type Signal } from "@preact/signals-core";
import type { DiffHunk, DiffSummary } from "./diff-model.ts";
import type { RepositorySummary } from "./api-client";
import type { DiffHighlights } from "./language";

export type DiffArea = "unstaged" | "staged";
export interface ChangedFile {
  path: string;
  index: string;
  worktree: string;
  kind?: string;
  conflicted?: boolean;
  untracked?: boolean;
}
export interface RepositoryState {
  fingerprint: string;
  branch: string;
  reviewMessage?: string;
  files: ChangedFile[];
}
export interface RepositoryFile {
  path: string;
  kind: string;
  size?: number;
}
export interface DiffResponse {
  fingerprint: string;
  preamble: string[];
  hunks: DiffHunk[];
  summary: DiffSummary;
}
export interface FileContent {
  text: string;
  fingerprint: string;
}
export interface CommentAnchor {
  path: string;
  area: string;
  side: string;
  line: number;
  context: string;
}
export interface ReviewComment extends CommentAnchor {
  id: string;
  body: string;
  created: string;
  resolved?: boolean;
  outdated?: boolean;
}
export type Selection = { kind: "diff"; path: string; area: DiffArea } | { kind: "file"; path: string };
export interface CommitDraft {
  subject: string;
  body: string;
}
export interface CommentDraft {
  anchor: CommentAnchor;
  body: string;
  error: string;
}
export interface FileSection {
  title: "Unstaged" | "Staged";
  area: DiffArea;
  files: ChangedFile[];
}

export interface AppSignals {
  repositories: Signal<RepositorySummary[]>;
  currentRepoID: Signal<string>;
  connected: Signal<boolean>;
  repository: Signal<RepositoryState | null>;
  selection: Signal<Selection | null>;
  diff: Signal<DiffResponse | null>;
  comments: Signal<ReviewComment[]>;
  splitView: Signal<boolean>;
  currentHunk: Signal<number>;
  commitDraft: Signal<CommitDraft>;
  commentDraft: Signal<CommentDraft | null>;
  error: Signal<string>;
  editorMode: Signal<boolean>;
  fileContent: Signal<FileContent | null>;
  browseMode: Signal<"changes" | "files">;
  repositoryFiles: Signal<RepositoryFile[]>;
  filesLoaded: Signal<boolean>;
  filesLoading: Signal<boolean>;
  fileFilter: Signal<string>;
  diffHighlights: Signal<DiffHighlights>;
  viewModel: ReadonlySignal<AppViewModel>;
}

export interface AppViewModel {
  repositories: RepositorySummary[];
  currentRepoID: string;
  connected: boolean;
  repository: RepositoryState | null;
  sections: FileSection[];
  selection: Selection | null;
  selectedFile: ChangedFile | null;
  diff: DiffResponse | null;
  parsedDiff: { preamble: string[]; hunks: DiffHunk[] };
  diffSummary: DiffSummary;
  comments: ReviewComment[];
  commentIndex: Map<string, ReviewComment[]>;
  activityComments: ReviewComment[];
  messageComments: ReviewComment[];
  splitView: boolean;
  currentHunk: number;
  commitDraft: CommitDraft;
  commentDraft: CommentDraft | null;
  error: string;
  editorMode: boolean;
  stageLabel: string;
  viewLabel: string;
  browseMode: "changes" | "files";
  repositoryFiles: RepositoryFile[];
  filteredRepositoryFiles: RepositoryFile[];
  filesLoaded: boolean;
  filesLoading: boolean;
  fileFilter: string;
  hasStagedFiles: boolean;
  sourceMode: boolean;
  diffHighlights: DiffHighlights;
}

export const anchorKey = (anchor: Pick<CommentAnchor, "path" | "area" | "side" | "line">) =>
  `${anchor.path}\u0000${anchor.area}\u0000${anchor.side}\u0000${anchor.line}`;

export function createAppSignals(): AppSignals {
  const repositories = signal<RepositorySummary[]>([]),
    currentRepoID = signal(""),
    connected = signal(false);
  const repository = signal<RepositoryState | null>(null),
    selection = signal<Selection | null>(null),
    diff = signal<DiffResponse | null>(null);
  const comments = signal<ReviewComment[]>([]),
    splitView = signal(false),
    currentHunk = signal(0);
  const commitDraft = signal<CommitDraft>({ subject: "", body: "" }),
    commentDraft = signal<CommentDraft | null>(null),
    error = signal("");
  const editorMode = signal(false),
    fileContent = signal<FileContent | null>(null);
  const browseMode = signal<"changes" | "files">("changes"),
    repositoryFiles = signal<RepositoryFile[]>([]);
  const filesLoaded = signal(false),
    filesLoading = signal(false),
    fileFilter = signal("");
  const diffHighlights = signal<DiffHighlights>(new Map());
  const viewModel = computed<AppViewModel>(() => {
    const repo = repository.value,
      selected = selection.value,
      diffValue = diff.value;
    const sections: FileSection[] = repo
      ? ([
          { title: "Unstaged", area: "unstaged", files: repo.files.filter((file) => file.worktree !== " ") },
          {
            title: "Staged",
            area: "staged",
            files: repo.files.filter((file) => file.index !== " " && file.index !== "?"),
          },
        ].filter((section) => section.files.length) as FileSection[])
      : [];
    const selectedFile =
      selected?.kind === "diff" ? repo?.files.find((file) => file.path === selected.path) || null : null;
    const parsedDiff = { preamble: diffValue?.preamble ?? [], hunks: diffValue?.hunks ?? [] },
      index = new Map<string, ReviewComment[]>();
    for (const comment of comments.value)
      index.set(anchorKey(comment), [...(index.get(anchorKey(comment)) || []), comment]);
    const activityComments = selected
      ? comments.value.filter(
          (comment) =>
            comment.path === selected.path &&
            (selected.kind === "file" || comment.line === 0 || Boolean(comment.outdated)),
        )
      : [];
    const query = fileFilter.value.trim().toLocaleLowerCase();
    return {
      repositories: repositories.value,
      currentRepoID: currentRepoID.value,
      connected: connected.value,
      repository: repo,
      sections,
      selection: selected,
      selectedFile,
      diff: diffValue,
      parsedDiff,
      diffSummary: diffValue?.summary ?? { additions: 0, deletions: 0, hunks: 0 },
      comments: comments.value,
      commentIndex: index,
      activityComments,
      messageComments: comments.value.filter((item) => item.path === "@commit-message"),
      splitView: splitView.value,
      currentHunk: Math.min(currentHunk.value, Math.max(0, parsedDiff.hunks.length - 1)),
      commitDraft: commitDraft.value,
      commentDraft: commentDraft.value,
      error: error.value,
      editorMode: editorMode.value,
      stageLabel: selected?.kind === "diff" && selected.area === "staged" ? "Unstage file" : "Stage file",
      viewLabel: splitView.value ? "Unified" : "Split",
      browseMode: browseMode.value,
      repositoryFiles: repositoryFiles.value,
      filteredRepositoryFiles: query
        ? repositoryFiles.value.filter((file) => file.path.toLocaleLowerCase().includes(query))
        : repositoryFiles.value,
      filesLoaded: filesLoaded.value,
      filesLoading: filesLoading.value,
      fileFilter: fileFilter.value,
      hasStagedFiles: Boolean(repo?.files.some((file) => file.index !== " " && file.index !== "?")),
      sourceMode: selected?.kind === "file",
      diffHighlights: diffHighlights.value,
    };
  });
  return {
    repositories,
    currentRepoID,
    connected,
    repository,
    selection,
    diff,
    comments,
    splitView,
    currentHunk,
    commitDraft,
    commentDraft,
    error,
    editorMode,
    fileContent,
    browseMode,
    repositoryFiles,
    filesLoaded,
    filesLoading,
    fileFilter,
    diffHighlights,
    viewModel,
  };
}

export function updateRepositoryState(state: AppSignals, repository: RepositoryState) {
  batch(() => {
    repository = { ...repository, files: repository.files ?? [] };
    state.repository.value = repository;
    if (
      state.selection.value?.kind === "diff" &&
      !repository.files.some((file) => file.path === state.selection.value?.path)
    ) {
      state.selection.value = null;
      state.diff.value = null;
      state.editorMode.value = false;
    }
  });
}

export function selectRepositoryFile(state: AppSignals, file: ChangedFile, area: DiffArea) {
  batch(() => {
    state.selection.value = { kind: "diff", path: file.path, area };
    state.diff.value = null;
    state.diffHighlights.value = new Map();
    state.fileContent.value = null;
    state.editorMode.value = false;
    state.currentHunk.value = 0;
    state.error.value = "";
  });
}

export function selectSourceFile(state: AppSignals, file: RepositoryFile) {
  batch(() => {
    state.selection.value = { kind: "file", path: file.path };
    state.diff.value = null;
    state.fileContent.value = null;
    state.editorMode.value = false;
    state.error.value = "";
  });
}
