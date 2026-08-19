import { html, nothing } from "lit-html";
import { repeat } from "lit-html/directives/repeat.js";
import { ref, type RefOrCallback } from "lit-html/directives/ref.js";
import type { AppViewModel, ChangedFile, CommentAnchor, DiffArea, RepositoryFile, ReviewComment } from "./app-model";
import type { ColorScheme } from "./theme";

export interface AppRefs {
  workspace: RefOrCallback;
  diff: RefOrCallback;
  editor: RefOrCallback;
  composer: RefOrCallback;
  hunk(index: number): RefOrCallback;
}
export interface AppActions {
  refs: AppRefs;
  refresh(): void;
  chooseRepository(id: string): void;
  selectFile(file: ChangedFile, area: DiffArea): void;
  changeArea(area: DiffArea): void;
  toggleView(): void;
  navigateHunk(direction: number): void;
  mutateFile(action: string): void;
  mutatePatch(scope: "hunk" | "lines", hunk: number, lines: number[], action?: "discard"): void;
  editFile(): void;
  saveFile(): void;
  cancelEdit(): void;
  openComment(anchor: CommentAnchor): void;
  closeComment(): void;
  updateCommentDraft(body: string): void;
  submitComment(): void;
  updateCommit(field: "subject" | "body", value: string): void;
  commit(event: SubmitEvent): void;
  toggleComment(comment: ReviewComment): void;
  deleteComment(comment: ReviewComment): void;
  showChanges(): void;
  showFiles(): void;
  filterFiles(value: string): void;
  selectSourceFile(file: RepositoryFile): void;
  stageAll(): void;
  setColorScheme(scheme: ColorScheme): void;
}

import { anchorKey, commentCard, commentEditor, diffTemplate } from "./review-templates";

export function reviewMessageTemplate(vm: AppViewModel, actions: AppActions) {
  const message = vm.repository?.reviewMessage || "";
  return html`<section id="review-message-panel" class="review-message" ?hidden=${!message}>
    <div class="review-message-heading">
      <div>
        <span class="eyebrow">Proposed commit message</span>
        <h2>Review the change description</h2>
      </div>
      <button
        id="comment-message"
        class="subtle"
        type="button"
        @click=${() => actions.openComment({ path: "@commit-message", area: "", side: "", line: 0, context: message })}
      >
        Add comment
      </button>
    </div>
    <pre
      id="review-message-text"
      @click=${() => actions.openComment({ path: "@commit-message", area: "", side: "", line: 0, context: message })}
    >
${message}</pre>
    ${commentEditor(vm, actions, "@commit-message")}
    <div id="review-message-comments">
      ${repeat(
        vm.messageComments,
        (comment) => comment.id,
        (comment) => commentCard(comment, actions),
      )}
    </div>
  </section>`;
}

export function activeTemplate(vm: AppViewModel, actions: AppActions) {
  if (vm.sourceMode) return sourceTemplate(vm, actions);
  const hunks = vm.parsedDiff.hunks;
  return html`<div class="file-header">
      <div class="file-title"><span class="file-icon">▤</span><strong id="path">${vm.selection?.path}</strong></div>
      <div class="file-actions">
        <select
          id="area"
          aria-label="Diff area"
          .value=${vm.selection?.kind === "diff" ? vm.selection.area : "unstaged"}
          @change=${(e: Event) => actions.changeArea((e.target as HTMLSelectElement).value as DiffArea)}
        >
          <option value="unstaged">Unstaged</option>
          <option value="staged">Staged</option>
        </select>
        <button id="view-toggle" class="subtle" type="button" @click=${actions.toggleView}>${vm.viewLabel}</button
        ><button id="edit" class="subtle" type="button" @click=${actions.editFile}>Edit file</button
        ><button
          id="comment-file"
          class="subtle"
          type="button"
          @click=${() => actions.openComment({ path: vm.selection?.path || "", area: "", side: "", line: 0, context: "" })}
        >
          File comment
        </button>
        <button
          id="stage"
          data-test-id="stage-file"
          class="primary"
          type="button"
          @click=${() => actions.mutateFile(vm.selection?.kind === "diff" && vm.selection.area === "staged" ? "unstage" : "stage")}
        >
          ${vm.stageLabel}</button
        ><button
          id="discard"
          class="danger subtle"
          type="button"
          ?hidden=${vm.selection?.kind === "diff" && vm.selection.area === "staged"}
          @click=${() => actions.mutateFile("discard")}
        >
          Discard file
        </button>
      </div>
    </div>
    <div id="message" role="status" ?hidden=${!vm.error}>${vm.error}</div>
    <div id="diff-toolbar" class="diff-toolbar" ?hidden=${!hunks.length || vm.editorMode}>
      <div id="diff-summary" class="diff-summary" aria-live="polite">
        <span class="additions">+${vm.diffSummary.additions}</span
        ><span class="deletions">−${vm.diffSummary.deletions}</span
        ><span class="hunks">${vm.diffSummary.hunks} ${vm.diffSummary.hunks === 1 ? "hunk" : "hunks"}</span>
      </div>
      <div class="hunk-navigation" aria-label="Hunk navigation">
        <button
          id="previous-hunk"
          class="subtle compact"
          type="button"
          title="Previous hunk (P)"
          ?disabled=${hunks.length < 2}
          @click=${() => actions.navigateHunk(-1)}
        >
          ↑ Previous</button
        ><span id="hunk-position">${hunks.length ? `Hunk ${vm.currentHunk + 1} of ${hunks.length}` : "No hunks"}</span
        ><button
          id="next-hunk"
          class="subtle compact"
          type="button"
          title="Next hunk (N)"
          ?disabled=${hunks.length < 2}
          @click=${() => actions.navigateHunk(1)}
        >
          Next ↓
        </button>
      </div>
    </div>
    <div
      id="diff"
      class=${`diff-surface ${vm.splitView ? "split" : "unified"}`}
      tabindex="0"
      aria-label="File diff"
      ?hidden=${vm.editorMode}
      ${ref(actions.refs.diff)}
      @keydown=${(e: KeyboardEvent) => {
        if (!e.target || (e.target as HTMLElement).closest("input, textarea, [contenteditable='true']")) return;
        const key = e.key.toLowerCase();
        if (key === "n" || key === "p") {
          e.preventDefault();
          actions.navigateHunk(key === "n" ? 1 : -1);
        }
      }}
    >
      ${diffTemplate(vm, actions)}
    </div>
    <div id="editor-panel" ?hidden=${!vm.editorMode}>
      <div id="editor" ${ref(actions.refs.editor)}></div>
      <div class="editor-actions">
        <button id="cancel-edit" class="subtle" type="button" @click=${actions.cancelEdit}>Cancel</button
        ><button id="save" class="primary" type="button" @click=${actions.saveFile}>Save changes</button>
      </div>
    </div>
    <section id="comments-panel" ?hidden=${!vm.activityComments.length}>
      <h2>Review activity</h2>
      <div id="comments">
        ${repeat(
          vm.activityComments,
          (comment) => comment.id,
          (comment) => commentCard(comment, actions),
        )}
      </div>
    </section>`;
}

function sourceTemplate(vm: AppViewModel, actions: AppActions) {
  return html`<div class="file-header">
      <div class="file-title"><span class="file-icon">▤</span><strong id="path">${vm.selection?.path}</strong></div>
      <div class="file-actions">
        <button
          id="comment-file"
          class="subtle"
          type="button"
          @click=${() => actions.openComment({ path: vm.selection?.path || "", area: "file", side: "new", line: 0, context: "" })}
        >
          File comment
        </button>
      </div>
    </div>
    <div id="message" role="status" ?hidden=${!vm.error}>${vm.error}</div>
    ${commentEditor(vm, actions, vm.selection?.path || "", true)}
    <div id="editor-panel" data-test-id="source-viewer"><div id="editor" ${ref(actions.refs.editor)}></div></div>
    ${
      vm.commentDraft?.anchor.area === "file" && vm.commentDraft.anchor.line > 0
        ? commentEditor(vm, actions, anchorKey(vm.commentDraft.anchor))
        : nothing
    }
    <section id="comments-panel" ?hidden=${!vm.activityComments.length}>
      <h2>Review activity</h2>
      <div id="comments">
        ${repeat(
          vm.activityComments,
          (comment) => comment.id,
          (comment) => commentCard(comment, actions),
        )}
      </div>
    </section>`;
}
