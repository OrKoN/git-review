import { batch } from "@preact/signals-core";
import { HubClient, RepositoryClient } from "./api-client";
import {
  createAppSignals,
  selectRepositoryFile,
  selectSourceFile,
  updateRepositoryState,
  type ChangedFile,
  type DiffArea,
  type DiffResponse,
  type FileContent,
  type RepositoryFile,
  type RepositoryState,
  type ReviewComment,
} from "./app-model";
import { highlightDiff } from "./language";

export type ScrollPosition = { workspaceTop: number; diffTop: number; diffLeft: number };
export const navigation = {
  scroll: null as ScrollPosition | null,
  hunk: null as number | null,
};
let scrollCapture = (): ScrollPosition => ({ workspaceTop: 0, diffTop: 0, diffLeft: 0 });
export function setScrollCapture(capture: () => ScrollPosition) {
  scrollCapture = capture;
}

export const state = createAppSignals();
const hubClient = new HubClient();
let repoClient: RepositoryClient | null = null;
let repoAbort: AbortController | null = null;
let languageGeneration = 0;
let resetEditor = () => {};

export function setEditorReset(reset: () => void) {
  resetEditor = reset;
}

export function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}
export function json(method: string, value: unknown): RequestInit {
  return { method, headers: { "Content-Type": "application/json" }, body: JSON.stringify(value) };
}
export async function api<T>(path: string, options: RequestInit = {}) {
  if (!repoClient) throw new Error("Select a connected repository first.");
  return repoClient.request<T>(path, options);
}
export function preserveScroll() {
  navigation.scroll = scrollCapture();
}

export async function loadRepositories() {
  const repositories = (await hubClient.repositories()).sort((a, b) =>
    `${a.name}@${a.host}`.localeCompare(`${b.name}@${b.host}`),
  );
  const requested = state.currentRepoID.value || new URLSearchParams(location.hash.slice(1)).get("repo") || "";
  state.repositories.value = repositories;
  if (!repositories.length) {
    disconnectRepository(requested);
    state.error.value = "No repository servers are connected.";
    return;
  }
  if (requested && !repositories.some((repo) => repo.id === requested)) {
    disconnectRepository(requested);
    state.error.value = "The selected repository is disconnected.";
    return;
  }
  const next = requested || repositories[0].id;
  if (!repoClient || next !== state.currentRepoID.value) await connectRepository(next);
}

export async function connectRepository(id: string) {
  disconnectRepository(id);
  if (!id) return;
  let client: RepositoryClient | null = null;
  try {
    client = new RepositoryClient(await hubClient.access(id));
    repoClient = client;
    state.connected.value = true;
    await loadComments();
    await refresh(true);
    state.error.value = "";
    void streamRepositoryEvents();
  } catch (error) {
    disconnectRepository(id);
    throw new Error(`Could not connect to the repository through the hub. ${errorMessage(error)}`);
  }
}
function disconnectRepository(id = state.currentRepoID.value) {
  repoAbort?.abort();
  repoAbort = null;
  repoClient = null;
  resetEditor();
  batch(() => {
    state.currentRepoID.value = id;
    state.connected.value = false;
    state.repository.value = null;
    state.selection.value = null;
    state.diff.value = null;
    state.comments.value = [];
    state.editorMode.value = false;
    state.fileContent.value = null;
    state.commentDraft.value = null;
    state.repositoryFiles.value = [];
    state.filesLoaded.value = false;
    state.browseMode.value = "changes";
  });
}
async function streamRepositoryEvents() {
  repoAbort?.abort();
  const controller = new AbortController();
  repoAbort = controller;
  const client = repoClient;
  if (!client) return;
  try {
    await client.events(controller.signal, async (event) => {
      if (event === "repository") {
        preserveScroll();
        await refresh();
        if (state.selection.value) await loadDiff(true);
      }
      if (event === "comments") {
        preserveScroll();
        await loadComments();
      }
    });
  } catch (error) {
    if (!controller.signal.aborted) state.error.value = `Repository disconnected: ${errorMessage(error)}`;
  }
}
export async function refresh(throwOnError = false) {
  try {
    const repository = await api<RepositoryState>("/api/repository");
    const wasEmpty = !state.commitDraft.value.subject;
    updateRepositoryState(state, repository);
    if (wasEmpty && repository.reviewMessage) {
      const [subject, ...body] = repository.reviewMessage.split("\n");
      state.commitDraft.value = { subject, body: body.join("\n").trim() };
    }
  } catch (error) {
    state.error.value = errorMessage(error);
    if (throwOnError) throw error;
  }
}
export async function loadComments() {
  const comments = await api<ReviewComment[]>("/api/comments");
  if (state.selection.value?.kind === "file") resetEditor();
  state.comments.value = comments;
}
export async function loadDiff(keepScroll = false) {
  const selection = state.selection.value;
  if (!selection || selection.kind !== "diff") return;
  if (keepScroll) preserveScroll();
  state.error.value = "";
  try {
    const response = await api<DiffResponse>(
      `/api/diff?path=${encodeURIComponent(selection.path)}&area=${selection.area}`,
    );
    state.diff.value = response;
    const generation = ++languageGeneration;
    state.diffHighlights.value = new Map();
    const highlights = await highlightDiff(selection.path, state.viewModel.value.parsedDiff.hunks);
    if (generation === languageGeneration && state.selection.value?.path === selection.path)
      state.diffHighlights.value = highlights;
  } catch (error) {
    batch(() => {
      state.diff.value = null;
      state.error.value = errorMessage(error);
    });
  }
}
export async function chooseFile(file: ChangedFile, area: DiffArea) {
  resetEditor();
  selectRepositoryFile(state, file, area);
  await loadComments();
  await loadDiff();
  await refresh();
}
export async function showFiles() {
  state.browseMode.value = "files";
  if (state.filesLoaded.value || state.filesLoading.value) return;
  state.filesLoading.value = true;
  try {
    state.repositoryFiles.value = (await api<RepositoryFile[]>("/api/files")) ?? [];
    state.filesLoaded.value = true;
  } catch (error) {
    state.error.value = errorMessage(error);
  } finally {
    state.filesLoading.value = false;
  }
}
export async function chooseSourceFile(file: RepositoryFile) {
  resetEditor();
  selectSourceFile(state, file);
  try {
    state.fileContent.value = await api<FileContent>(`/api/file?path=${encodeURIComponent(file.path)}`);
    await loadComments();
  } catch (error) {
    state.error.value = errorMessage(error);
  }
}
export async function mutatePatch(scope: "hunk" | "lines", hunk: number, lines: number[], requested?: "discard") {
  const selected = state.selection.value,
    diff = state.diff.value;
  if (!selected || selected.kind !== "diff" || !diff) return;
  if (
    requested === "discard" &&
    !confirm(`Discard this ${scope === "hunk" ? "hunk" : "change block"} in ${selected.path}?`)
  )
    return;
  const action = requested || (selected.area === "staged" ? "unstage" : "stage");
  try {
    const queue = state.viewModel.value.sections.find((section) => section.area === "unstaged")?.files ?? [];
    const queueIndex = queue.findIndex((file) => file.path === selected.path);
    await api(
      "/api/change",
      json("POST", { action, path: selected.path, scope, fingerprint: diff.fingerprint, hunk, lines }),
    );
    preserveScroll();
    await refresh();
    if (action === "stage") {
      const remaining = state.viewModel.value.sections.find((section) => section.area === "unstaged")?.files ?? [];
      if (!remaining.some((file) => file.path === selected.path)) {
        const next = remaining.length ? remaining[Math.min(queueIndex, remaining.length - 1)] : null;
        if (next) selectRepositoryFile(state, next, "unstaged");
        else state.selection.value = null;
      }
    }
    if (state.selection.value?.kind === "diff") await loadDiff(true);
  } catch (error) {
    state.error.value = errorMessage(error);
  }
}
export async function mutateFile(action: string) {
  const selected = state.selection.value,
    diff = state.diff.value;
  if (!selected || selected.kind !== "diff" || !diff) return;
  if (action === "discard" && !confirm(`Discard all unstaged changes in ${selected.path}? This cannot be undone.`))
    return;
  try {
    const queue =
      state.viewModel.value.sections.find((section) => section.area === "unstaged")?.files.map((file) => file.path) ??
      [];
    const index = queue.indexOf(selected.path);
    await api("/api/change", json("POST", { action, path: selected.path, fingerprint: diff.fingerprint }));
    preserveScroll();
    await refresh();
    if (action === "stage") {
      const remaining = state.viewModel.value.sections.find((section) => section.area === "unstaged")?.files ?? [];
      const remainingPaths = new Set(remaining.map((file) => file.path));
      let next: ChangedFile | null = null;
      for (let offset = 1; offset < queue.length; offset++) {
        const path = queue[(index + offset) % queue.length];
        if (path !== selected.path && remainingPaths.has(path)) {
          next = remaining.find((file) => file.path === path) || null;
          break;
        }
      }
      if (next) selectRepositoryFile(state, next, "unstaged");
      else state.selection.value = null;
    } else if (action === "unstage") state.selection.value = { ...selected, area: "unstaged" };
    if (state.selection.value?.kind === "diff") await loadDiff(true);
  } catch (error) {
    state.error.value = errorMessage(error);
  }
}

export function closeRepositoryStream() {
  repoAbort?.abort();
  repoAbort = null;
}
