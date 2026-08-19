import { html, nothing, type TemplateResult } from "lit-html";
import { repeat } from "lit-html/directives/repeat.js";
import { ref } from "lit-html/directives/ref.js";
import type { AppViewModel, ChangedFile, DiffArea } from "./app-model";
import type { AppActions } from "./app-view";
import { activeTemplate, reviewMessageTemplate } from "./app-view";
import { buildFileTree, fileName, type FileTreeDirectory } from "./file-tree.ts";
import { loadColorScheme, type ColorScheme } from "./theme";

const statusLabel = (file: ChangedFile, area: DiffArea) => {
  if (file.conflicted) return "Conflict";
  if (file.untracked) return "New";
  const code = area === "staged" ? file.index : file.worktree;
  return ({ A: "Added", D: "Deleted", M: "Modified", R: "Renamed", T: "Type" } as Record<string, string>)[code] || code;
};
function fileTreeTemplate(directory: FileTreeDirectory, vm: AppViewModel, actions: AppActions): TemplateResult {
  return html`${repeat(
    directory.directories,
    (child) => child.path,
    (child) =>
      html`<details class="file-tree-directory" open>
        <summary><span class="directory-icon" aria-hidden="true"></span><span>${child.name}</span></summary>
        <div class="file-tree-children">${fileTreeTemplate(child, vm, actions)}</div>
      </details>`,
  )}${repeat(
    directory.files,
    (file) => file.path,
    (file) =>
      html`<button
        type="button"
        class=${`file tree-file${vm.selection?.kind === "file" && vm.selection.path === file.path ? " active" : ""}`}
        data-test-id="repository-file"
        data-path=${file.path}
        title=${file.path}
        @click=${() => actions.selectSourceFile(file)}
      >
        <span class="file-kind">▤</span><span class="file-name">${fileName(file)}</span>
      </button>`,
  )}`;
}

export function appTemplate(vm: AppViewModel, actions: AppActions) {
  const disconnectedOption = vm.currentRepoID && !vm.repositories.some((repo) => repo.id === vm.currentRepoID);
  return html` <header class="app-header">
      <div class="brand"><span class="brand-mark">GR</span><strong>git-review</strong></div>
      <label class="repository-picker"
        ><span>Repository</span
        ><select
          id="repository-select"
          aria-label="Connected repository"
          .value=${vm.currentRepoID}
          @change=${(event: Event) => actions.chooseRepository((event.target as HTMLSelectElement).value)}
        >
          ${disconnectedOption ? html`<option value=${vm.currentRepoID}>Selected repository · disconnected</option>` : nothing}
          ${!vm.repositories.length && !disconnectedOption ? html`<option value="">No repositories connected</option>` : nothing}
          ${repeat(
            vm.repositories,
            (repo) => repo.id,
            (repo) =>
              html`<option value=${repo.id}>
                ${repo.name} · ${repo.host}${repo.branch ? ` · ${repo.branch}` : ""}
              </option>`,
          )}
        </select></label
      >
      <span id="branch" class="branch-pill">${vm.repository?.branch || (vm.connected ? "detached HEAD" : "")}</span>
      <span class="session-badge"><span class="status-dot"></span>Live review</span>
      <select
        class="scheme-picker"
        aria-label="Color scheme"
        title="Color scheme"
        .value=${loadColorScheme()}
        @change=${(event: Event) => actions.setColorScheme((event.target as HTMLSelectElement).value as ColorScheme)}
      >
        <option value="system">System theme</option>
        <option value="light">Light theme</option>
        <option value="dark">Dark theme</option>
      </select>
      <button
        id="refresh"
        class="icon-button"
        type="button"
        title="Refresh repository"
        aria-label="Refresh repository"
        @click=${actions.refresh}
      >
        ↻
      </button>
    </header>
    <main>
      <aside class="sidebar" data-test-id="sidebar">
        <div class="sidebar-tabs" role="tablist">
          <button
            type="button"
            role="tab"
            data-test-id="changes-tab"
            aria-selected=${vm.browseMode === "changes"}
            @click=${actions.showChanges}
          >
            Changes</button
          ><button
            type="button"
            role="tab"
            data-test-id="files-tab"
            aria-selected=${vm.browseMode === "files"}
            @click=${actions.showFiles}
          >
            Files
          </button>
        </div>
        ${
          vm.browseMode === "changes"
            ? html`<div class="sidebar-heading">
                  <h2>Changes</h2>
                  <span id="change-count">${vm.repository?.files.length ?? ""}</span
                  ><button
                    type="button"
                    class="subtle compact"
                    data-test-id="stage-all"
                    ?disabled=${!vm.repository?.files.some((file) => file.worktree !== " ")}
                    @click=${actions.stageAll}
                  >
                    Stage all
                  </button>
                </div>
                <div id="files" data-test-id="changes-list">
                  ${vm.sections.map(
                    (section) =>
                      html` <div class="file-section-heading">
                          <span>${section.title}</span><span>${section.files.length}</span>
                        </div>
                        ${repeat(
                          section.files,
                          (file) => `${section.area}:${file.path}`,
                          (file) => {
                            const status = statusLabel(file, section.area),
                              active =
                                vm.selection?.kind === "diff" &&
                                vm.selection.path === file.path &&
                                vm.selection.area === section.area;
                            return html`<button
                              type="button"
                              class=${`file${active ? " active" : ""}`}
                              data-test-id="change-file"
                              data-path=${file.path}
                              data-area=${section.area}
                              @click=${() => actions.selectFile(file, section.area)}
                            >
                              <span class="file-kind">${file.kind === "symlink" ? "↗" : "▤"}</span
                              ><span class="file-name">${file.path}</span
                              ><span class=${`status status-${status.toLowerCase()}`}>${status}</span>
                            </button>`;
                          },
                        )}`,
                  )}
                </div>
                <form id="commit-form" data-test-id="commit-form" @submit=${actions.commit}>
                  <h2>Commit changes</h2>
                  <input
                    id="subject"
                    placeholder="Commit summary"
                    required
                    autocomplete="off"
                    .value=${vm.commitDraft.subject}
                    @input=${(e: Event) => actions.updateCommit("subject", (e.target as HTMLInputElement).value)}
                  /><textarea
                    id="body"
                    placeholder="Description (optional)"
                    .value=${vm.commitDraft.body}
                    @input=${(e: Event) => actions.updateCommit("body", (e.target as HTMLTextAreaElement).value)}
                  ></textarea
                  ><button class="primary full-width" ?disabled=${!vm.hasStagedFiles} aria-describedby="commit-help">
                    Commit staged changes
                  </button>
                  <p id="commit-help" class="form-help">
                    ${vm.hasStagedFiles ? "" : "Stage at least one file before committing."}
                  </p>
                </form>`
            : html`<div class="file-browser">
                <input
                  data-test-id="file-filter"
                  type="search"
                  placeholder="Filter files…"
                  .value=${vm.fileFilter}
                  @input=${(event: Event) => actions.filterFiles((event.target as HTMLInputElement).value)}
                />
                <div class="file-tree" data-test-id="repository-files">
                  ${
                    vm.filesLoading
                      ? html`<p>Loading files…</p>`
                      : fileTreeTemplate(buildFileTree(vm.filteredRepositoryFiles), vm, actions)
                  }
                </div>
              </div>`
        }
      </aside>
      <section id="workspace" ${ref(actions.refs.workspace)}>
        ${reviewMessageTemplate(vm, actions)}
        <div id="empty" ?hidden=${Boolean(vm.selection)}>
          <div class="empty-icon">⌘</div>
          <h1>Review remote changes</h1>
          <p id="empty-description" class=${vm.error ? "error" : ""}>
            ${vm.error || (vm.connected ? "Select a file from the sidebar to inspect its diff." : "No repository servers are connected.")}
          </p>
        </div>
        <div id="active" ?hidden=${!vm.selection}>${vm.selection ? activeTemplate(vm, actions) : nothing}</div>
      </section>
    </main>`;
}
