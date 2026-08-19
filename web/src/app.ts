import { EditorState, StateEffect } from "@codemirror/state";
import { EditorView, keymap, lineNumbers } from "@codemirror/view";
import { oneDark } from "@codemirror/theme-one-dark";
import { batch, effect } from "@preact/signals-core";
import { render } from "lit-html";
import { type CommentAnchor, type FileContent, type ReviewComment } from "./app-model";
import { appTemplate } from "./app-shell";
import type { AppActions } from "./app-view";
import { commentGutter } from "./editor-gutter";
import { loadLanguage, tokenHighlighting } from "./language";
import { applyColorScheme, loadColorScheme } from "./theme";
import {
  api,
  chooseFile,
  chooseSourceFile,
  closeRepositoryStream,
  connectRepository,
  errorMessage,
  json,
  loadComments,
  loadDiff,
  loadRepositories,
  mutateFile,
  mutatePatch,
  navigation,
  preserveScroll,
  refresh,
  setEditorReset,
  setScrollCapture,
  showFiles,
  state,
} from "./repository-controller";

const appRoot = document.querySelector<HTMLElement>("#app");
if (!appRoot) throw new Error("Missing #app mount point");
applyColorScheme(loadColorScheme());

let hubEvents: EventSource | null = null,
  editorView: EditorView | null = null;
let workspaceElement: HTMLElement | null = null,
  diffElement: HTMLElement | null = null,
  editorElement: HTMLElement | null = null,
  composerElement: HTMLTextAreaElement | null = null;
const hunkElements = new Map<number, HTMLElement>();
let pendingFocus = false;
let editorGeneration = 0;

setEditorReset(() => {
  editorView?.destroy();
  editorView = null;
});
setScrollCapture(() => ({
  workspaceTop: workspaceElement?.scrollTop || 0,
  diffTop: diffElement?.scrollTop || 0,
  diffLeft: diffElement?.scrollLeft || 0,
}));

async function editFile() {
  const selected = state.selection.value;
  if (!selected || selected.kind !== "diff") return;
  try {
    const content = await api<FileContent>(`/api/file?path=${encodeURIComponent(selected.path)}`);
    batch(() => {
      state.fileContent.value = content;
      state.editorMode.value = true;
      state.error.value = "";
    });
  } catch (error) {
    state.error.value = errorMessage(error);
  }
}
async function saveFile() {
  const selected = state.selection.value,
    content = state.fileContent.value;
  if (!selected || selected.kind !== "diff" || !content) return;
  try {
    await api<FileContent>(
      `/api/file?path=${encodeURIComponent(selected.path)}`,
      json("PUT", { text: editorView?.state.doc.toString() || "", fingerprint: content.fingerprint }),
    );
    editorView?.destroy();
    editorView = null;
    state.editorMode.value = false;
    await loadDiff(true);
    await refresh();
  } catch (error) {
    state.error.value = errorMessage(error);
  }
}
function openComment(anchor: CommentAnchor) {
  state.commentDraft.value = { anchor, body: "", error: "" };
  pendingFocus = true;
}
async function submitComment() {
  const draft = state.commentDraft.value;
  if (!draft) return;
  const body = draft.body.trim();
  if (!body) {
    state.commentDraft.value = { ...draft, error: "Enter a comment before submitting." };
    pendingFocus = true;
    return;
  }
  try {
    await api("/api/comments", json("POST", { ...draft.anchor, body }));
    preserveScroll();
    state.commentDraft.value = null;
    await loadComments();
  } catch (error) {
    state.commentDraft.value = { ...draft, error: errorMessage(error) };
  }
}
async function updateComment(comment: ReviewComment, options: RequestInit) {
  try {
    await api(`/api/comments/${comment.id}`, options);
    preserveScroll();
    await loadComments();
  } catch (error) {
    state.error.value = errorMessage(error);
  }
}

const actions: AppActions = {
  refs: {
    workspace: (element) => {
      workspaceElement = element as HTMLElement | null;
    },
    diff: (element) => {
      diffElement = element as HTMLElement | null;
    },
    editor: (element) => {
      editorElement = element as HTMLElement | null;
    },
    composer: (element) => {
      composerElement = element as HTMLTextAreaElement | null;
    },
    hunk: (index) => (element) => {
      if (element) hunkElements.set(index, element as HTMLElement);
      else hunkElements.delete(index);
    },
  },
  refresh: () => void refresh(),
  chooseRepository: (id) =>
    void connectRepository(id).catch((error) => {
      state.error.value = errorMessage(error);
    }),
  selectFile: (file, area) => void chooseFile(file, area),
  showChanges: () => {
    state.browseMode.value = "changes";
  },
  showFiles: () => void showFiles(),
  filterFiles: (value) => {
    state.fileFilter.value = value;
  },
  selectSourceFile: (file) => void chooseSourceFile(file),
  stageAll: () =>
    void (async () => {
      try {
        await api(
          "/api/change",
          json("POST", { action: "stage_all", fingerprint: state.repository.value?.fingerprint }),
        );
        await refresh();
      } catch (error) {
        state.error.value = errorMessage(error);
      }
    })(),
  setColorScheme: applyColorScheme,
  changeArea: (area) => {
    const selection = state.selection.value;
    if (selection?.kind === "diff") state.selection.value = { ...selection, area };
    void loadDiff();
  },
  toggleView: () => {
    preserveScroll();
    state.splitView.value = !state.splitView.value;
  },
  navigateHunk: (direction) => {
    const total = state.viewModel.value.parsedDiff.hunks.length;
    if (!total) return;
    state.currentHunk.value = (state.currentHunk.value + direction + total) % total;
    navigation.hunk = state.currentHunk.value;
  },
  mutateFile: (action) => void mutateFile(action),
  mutatePatch: (scope, hunk, lines, action) => void mutatePatch(scope, hunk, lines, action),
  editFile: () => void editFile(),
  saveFile: () => void saveFile(),
  cancelEdit: () => {
    editorView?.destroy();
    editorView = null;
    state.editorMode.value = false;
  },
  openComment,
  closeComment: () => {
    state.commentDraft.value = null;
  },
  updateCommentDraft: (body) => {
    const draft = state.commentDraft.value;
    if (draft) state.commentDraft.value = { ...draft, body, error: "" };
  },
  submitComment: () => void submitComment(),
  updateCommit: (field, value) => {
    state.commitDraft.value = { ...state.commitDraft.value, [field]: value };
  },
  commit: (event) => {
    event.preventDefault();
    void (async () => {
      try {
        const draft = state.commitDraft.value;
        await api("/api/commit", json("POST", { ...draft, fingerprint: state.repository.value?.fingerprint }));
        state.commitDraft.value = { subject: "", body: "" };
        await refresh();
      } catch (error) {
        state.error.value = errorMessage(error);
      }
    })();
  },
  toggleComment: (comment) => void updateComment(comment, json("PATCH", { resolved: !comment.resolved })),
  deleteComment: (comment) => void updateComment(comment, { method: "DELETE" }),
};

const disposeRender = effect(() => {
  const vm = state.viewModel.value;
  if (!vm.editorMode && !vm.sourceMode && editorView) {
    editorView.destroy();
    editorView = null;
  }
  render(appTemplate(vm, actions), appRoot);
  if ((vm.editorMode || vm.sourceMode) && editorElement && !editorView && state.fileContent.value) {
    const generation = ++editorGeneration,
      selection = vm.selection;
    const readOnly = vm.sourceMode;
    const editorTheme = matchMedia("(prefers-color-scheme: dark)").matches ? oneDark : tokenHighlighting;
    const sourceComments = readOnly ? commentGutter(state.comments.value, selection, openComment) : [];
    editorView = new EditorView({
      state: EditorState.create({
        doc: state.fileContent.value.text,
        extensions: [
          lineNumbers(),
          sourceComments,
          EditorView.lineWrapping,
          EditorState.readOnly.of(readOnly),
          EditorView.editable.of(!readOnly),
          keymap.of([]),
          editorTheme,
        ],
      }),
      parent: editorElement,
    });
    void loadLanguage(selection?.path || "").then((extension) => {
      if (generation === editorGeneration && editorView)
        editorView.dispatch({ effects: StateEffect.appendConfig.of(extension) });
    });
  }
  if (navigation.scroll) {
    if (workspaceElement) workspaceElement.scrollTop = navigation.scroll.workspaceTop;
    if (diffElement) {
      diffElement.scrollTop = navigation.scroll.diffTop;
      diffElement.scrollLeft = navigation.scroll.diffLeft;
    }
    navigation.scroll = null;
  }
  if (pendingFocus) {
    composerElement?.focus();
    pendingFocus = false;
  }
  if (navigation.hunk !== null) {
    hunkElements.get(navigation.hunk)?.scrollIntoView({ behavior: "smooth", block: "start" });
    navigation.hunk = null;
  }
});

try {
  await loadRepositories();
  hubEvents = new EventSource("/api/events");
  hubEvents.addEventListener(
    "repositories",
    () =>
      void loadRepositories().catch((error) => {
        state.error.value = errorMessage(error);
      }),
  );
} catch (error) {
  state.error.value = `git-review hub failed: ${errorMessage(error)}`;
}
addEventListener(
  "pagehide",
  () => {
    disposeRender();
    closeRepositoryStream();
    hubEvents?.close();
    editorView?.destroy();
  },
  { once: true },
);
