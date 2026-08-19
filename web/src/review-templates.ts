import { html, nothing, type TemplateResult } from "lit-html";
import { repeat } from "lit-html/directives/repeat.js";
import { ref } from "lit-html/directives/ref.js";
import type { AppViewModel, CommentAnchor, ReviewComment } from "./app-model";
import type { AppActions } from "./app-view";
import type { DiffHunk, DiffLine } from "./diff-model";
import { highlightKey } from "./language";

const anchorFor = (vm: AppViewModel, line: DiffLine, side?: "old" | "new"): CommentAnchor => {
  const actualSide = side || (line.type === "del" ? "old" : "new");
  return {
    path: vm.selection?.path || "",
    area: vm.selection?.kind === "diff" ? vm.selection.area : "",
    side: actualSide,
    line: actualSide === "old" ? line.oldNo || 0 : line.newNo || 0,
    context: line.raw,
  };
};
export const anchorKey = (anchor: CommentAnchor) =>
  `${anchor.path}\u0000${anchor.area}\u0000${anchor.side}\u0000${anchor.line}`;

export function diffTemplate(vm: AppViewModel, actions: AppActions) {
  if (!vm.diff?.hunks.length && !vm.diff?.preamble.length)
    return html`${commentEditor(vm, actions, vm.selection?.path || "", true)}
      <div class="diff-empty">No changes in this area.</div>`;
  return html`${commentEditor(vm, actions, vm.selection?.path || "", true)}${vm.parsedDiff.preamble.length ? html`<div class="diff-notice">${vm.parsedDiff.preamble.join("\n")}</div>` : nothing}${repeat(
    vm.parsedDiff.hunks,
    (hunk) => hunk.index,
    (hunk) => hunkTemplate(vm, actions, hunk),
  )}`;
}
function hunkTemplate(vm: AppViewModel, actions: AppActions, hunk: DiffHunk) {
  const area = vm.selection?.kind === "diff" ? vm.selection.area : "unstaged";
  return html`<section class="diff-hunk" data-hunk=${hunk.index} ${ref(actions.refs.hunk(hunk.index))}>
    <div class="hunk-header">
      <span>${hunk.header}</span>
      <div class="hunk-actions">
        <button
          type="button"
          class="primary compact"
          ?disabled=${Boolean(vm.selectedFile?.conflicted)}
          @click=${() => actions.mutatePatch("hunk", hunk.index, [])}
        >
          ${area === "staged" ? "Unstage hunk" : "Stage hunk"}</button
        >${area === "unstaged" ? html`<button type="button" class="danger compact" ?disabled=${Boolean(vm.selectedFile?.conflicted)} @click=${() => actions.mutatePatch("hunk", hunk.index, [], "discard")}>Discard hunk</button>` : nothing}
      </div>
    </div>
    ${vm.splitView ? splitHunk(vm, actions, hunk) : unifiedHunk(vm, actions, hunk)}
  </section>`;
}
function unifiedHunk(vm: AppViewModel, actions: AppActions, hunk: DiffHunk) {
  return hunk.lines.map((line, index) =>
    line.type === "meta"
      ? html`<div class="diff-meta">${line.text}</div>`
      : html`<div class=${`diff-row ${line.type}`}>
            <button
              type="button"
              class="comment-trigger"
              title="Add line comment"
              @click=${() => actions.openComment(anchorFor(vm, line))}
            >
              +</button
            >${lineNumber(vm, actions, line, "old")}${lineNumber(vm, actions, line, "new")}<span class="diff-marker"
              >${line.type === "add" ? "+" : line.type === "del" ? "−" : ""}</span
            ><span class="diff-code" @click=${() => actions.openComment(anchorFor(vm, line))}
              >${highlightedCode(vm, hunk.index, index, line.type === "del" ? "old" : "new", line.text)}</span
            >
          </div>
          ${threads(vm, actions, [line.oldNo ? anchorFor(vm, line, "old") : null, line.newNo ? anchorFor(vm, line, "new") : null])}${commentEditor(vm, actions, anchorKey(anchorFor(vm, line)))}`,
  );
}
function lineNumber(vm: AppViewModel, actions: AppActions, line: DiffLine, side: "old" | "new") {
  const number = side === "old" ? line.oldNo : line.newNo;
  return number
    ? html`<button type="button" class="line-number" @click=${() => actions.openComment(anchorFor(vm, line, side))}>
        ${number}
      </button>`
    : html`<span class="line-number"></span>`;
}
function splitHunk(vm: AppViewModel, actions: AppActions, hunk: DiffHunk) {
  const rows: TemplateResult[] = [];
  for (let index = 0; index < hunk.lines.length;) {
    const line = hunk.lines[index];
    if (line.type === "meta") {
      rows.push(html`<div class="diff-meta split-span">${line.text}</div>`);
      index++;
      continue;
    }
    if (line.type === "context") {
      rows.push(splitRow(vm, actions, hunk, line, line));
      index++;
      continue;
    }
    const block: DiffLine[] = [];
    while (index < hunk.lines.length && ["add", "del"].includes(hunk.lines[index].type))
      block.push(hunk.lines[index++]);
    const dels = block.filter((item) => item.type === "del"),
      adds = block.filter((item) => item.type === "add");
    for (let row = 0; row < Math.max(dels.length, adds.length); row++)
      rows.push(
        splitRow(
          vm,
          actions,
          hunk,
          dels[row] || null,
          adds[row] || null,
          row === 0
            ? { hunk: hunk.index, ordinals: block.map((item) => item.ordinal || 0).sort((a, b) => a - b) }
            : undefined,
        ),
      );
  }
  return rows;
}
function splitRow(
  vm: AppViewModel,
  actions: AppActions,
  hunk: DiffHunk,
  oldLine: DiffLine | null,
  newLine: DiffLine | null,
  block?: { hunk: number; ordinals: number[] },
) {
  const anchors = [oldLine ? anchorFor(vm, oldLine, "old") : null, newLine ? anchorFor(vm, newLine, "new") : null],
    area = vm.selection?.kind === "diff" ? vm.selection.area : "unstaged";
  return html`<div class="split-row">
      ${splitCell(vm, actions, oldLine, "old", hunk)}
      <div class="block-gutter">
        ${block ? html`<button type="button" class="block-button apply" title=${area === "staged" ? "Unstage this block" : "Stage this block"} ?disabled=${Boolean(vm.selectedFile?.conflicted)} @click=${() => actions.mutatePatch("lines", block.hunk, block.ordinals)}>${area === "staged" ? "←" : "→"}</button>${area === "unstaged" ? html`<button type="button" class="block-button reject" title="Discard this block" ?disabled=${Boolean(vm.selectedFile?.conflicted)} @click=${() => actions.mutatePatch("lines", block.hunk, block.ordinals, "discard")}>×</button>` : nothing}` : nothing}
      </div>
      ${splitCell(vm, actions, newLine, "new", hunk)}
    </div>
    ${threads(vm, actions, anchors)}${anchors.map((anchor) => (anchor ? commentEditor(vm, actions, anchorKey(anchor)) : nothing))}`;
}
function splitCell(vm: AppViewModel, actions: AppActions, line: DiffLine | null, side: "old" | "new", hunk?: DiffHunk) {
  if (!line) return html`<div class="split-cell empty"><span></span><span></span><span></span></div>`;
  const anchor = anchorFor(vm, line, side);
  return html`<div class=${`split-cell ${line.type}`}>
    <button type="button" class="comment-trigger" title="Add line comment" @click=${() => actions.openComment(anchor)}>
      +</button
    ><button type="button" class="line-number" @click=${() => actions.openComment(anchor)}>
      ${side === "old" ? line.oldNo : line.newNo}</button
    ><span class="diff-code" @click=${() => actions.openComment(anchor)}
      >${hunk ? highlightedCode(vm, hunk.index, hunk.lines.indexOf(line), side, line.text) : line.text || " "}</span
    >
  </div>`;
}

function highlightedCode(vm: AppViewModel, hunk: number, line: number, side: "old" | "new", fallback: string) {
  const segments = vm.diffHighlights.get(highlightKey(hunk, side, line));
  if (!segments) return fallback || " ";
  return segments.map((segment) =>
    segment.classes ? html`<span class=${segment.classes}>${segment.text}</span>` : segment.text,
  );
}
function threads(vm: AppViewModel, actions: AppActions, anchors: Array<CommentAnchor | null>) {
  const seen = new Set<string>();
  return anchors.flatMap((anchor) => {
    if (!anchor || seen.has(anchorKey(anchor))) return [];
    seen.add(anchorKey(anchor));
    return (vm.commentIndex.get(anchorKey(anchor)) || []).map(
      (comment) => html`<div class="inline-thread">${commentCard(comment, actions)}</div>`,
    );
  });
}

export function commentEditor(vm: AppViewModel, actions: AppActions, location: string, root = false) {
  const draft = vm.commentDraft;
  if (
    !draft ||
    (root
      ? !(draft.anchor.line === 0 && draft.anchor.path === location)
      : draft.anchor.path === "@commit-message"
        ? location !== "@commit-message"
        : anchorKey(draft.anchor) !== location)
  )
    return nothing;
  const title =
    draft.anchor.path === "@commit-message"
      ? "Comment on commit message"
      : draft.anchor.line
        ? `Comment on ${draft.anchor.side} line ${draft.anchor.line}`
        : "Comment on file";
  return html`<div class="inline-comment-editor">
    <div class="inline-comment-heading"><strong>${title}</strong><span>This will stream to the CLI</span></div>
    <textarea
      maxlength="8192"
      placeholder="Leave review feedback…"
      .value=${draft.body}
      ${ref(actions.refs.composer)}
      @input=${(e: Event) => actions.updateCommentDraft((e.target as HTMLTextAreaElement).value)}
      @keydown=${(e: KeyboardEvent) => {
        if (e.key === "Escape") {
          e.preventDefault();
          actions.closeComment();
        }
      }}
    ></textarea>
    <div class="comment-error">${draft.error}</div>
    <div class="inline-comment-actions">
      <button type="button" class="subtle" data-action="cancel-comment" @click=${actions.closeComment}>Cancel</button
      ><button type="button" class="primary" @click=${actions.submitComment}>Add comment</button>
    </div>
  </div>`;
}
export function commentCard(comment: ReviewComment, actions: AppActions) {
  return html`<article class=${`comment-card${comment.resolved ? " resolved" : ""}`}>
    <header>
      <span class="review-avatar">R</span
      ><span
        >${comment.outdated ? "Reviewer · outdated" : `Reviewer · ${new Date(comment.created).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`}</span
      >
    </header>
    <p>${comment.body}</p>
    <div class="comment-card-actions">
      <button type="button" class="link-button" @click=${() => actions.toggleComment(comment)}>
        ${comment.resolved ? "Reopen" : "Resolve"}</button
      ><button type="button" class="link-button danger-text" @click=${() => actions.deleteComment(comment)}>
        Delete
      </button>
    </div>
  </article>`;
}
