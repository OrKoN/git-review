import assert from "node:assert/strict";
import { test } from "node:test";
import { batch, effect } from "@preact/signals-core";
import { createAppSignals, selectRepositoryFile, updateRepositoryState, type RepositoryState } from "./app-model.ts";

const repository: RepositoryState = {
  fingerprint: "one",
  branch: "main",
  files: [
    { path: "both.ts", index: "M", worktree: "M" },
    { path: "only-index.ts", index: "A", worktree: " " },
  ],
};

test("derives unstaged before staged and duplicates partially staged files", () => {
  const state = createAppSignals();
  updateRepositoryState(state, repository);
  assert.deepEqual(
    state.viewModel.value.sections.map((section) => [section.title, section.files.map((file) => file.path)]),
    [
      ["Unstaged", ["both.ts"]],
      ["Staged", ["both.ts", "only-index.ts"]],
    ],
  );
});

test("derives selection, comments, parsed diff, and summary", () => {
  const state = createAppSignals();
  updateRepositoryState(state, repository);
  selectRepositoryFile(state, repository.files[0], "unstaged");
  state.diff.value = {
    fingerprint: "d",
    preamble: [],
    hunks: [
      {
        index: 0,
        header: "@@ -1 +1 @@",
        lines: [
          { type: "del", raw: "-old", text: "old", oldNo: 1, newNo: null, ordinal: 0 },
          { type: "add", raw: "+new", text: "new", oldNo: null, newNo: 1, ordinal: 1 },
        ],
      },
    ],
    summary: { additions: 1, deletions: 1, hunks: 1 },
  };
  state.comments.value = [
    {
      id: "c",
      path: "both.ts",
      area: "unstaged",
      side: "new",
      line: 1,
      context: "+new",
      body: "note",
      created: "2026-01-01",
    },
  ];
  assert.equal(state.viewModel.value.selectedFile?.path, "both.ts");
  assert.deepEqual(state.viewModel.value.diffSummary, { additions: 1, deletions: 1, hunks: 1 });
  assert.equal([...state.viewModel.value.commentIndex.values()][0][0].body, "note");
});

test("batch exposes only a coherent final state", () => {
  const state = createAppSignals();
  const observations: string[] = [];
  const dispose = effect(() => {
    observations.push(`${state.currentRepoID.value}:${state.connected.value}`);
  });
  batch(() => {
    state.currentRepoID.value = "repo";
    state.connected.value = true;
  });
  dispose();
  assert.deepEqual(observations, [":false", "repo:true"]);
});

test("repository updates retain drafts and display preferences", () => {
  const state = createAppSignals();
  state.commitDraft.value = { subject: "subject", body: "body" };
  state.splitView.value = true;
  updateRepositoryState(state, repository);
  updateRepositoryState(state, { ...repository, fingerprint: "two" });
  assert.deepEqual(state.commitDraft.value, { subject: "subject", body: "body" });
  assert.equal(state.splitView.value, true);
});

test("normalizes nullable file arrays from older repository servers", () => {
  const state = createAppSignals();
  updateRepositoryState(state, { fingerprint: "clean", branch: "main", files: null } as unknown as RepositoryState);
  assert.deepEqual(state.repository.value?.files, []);
  assert.deepEqual(state.viewModel.value.sections, []);
});
