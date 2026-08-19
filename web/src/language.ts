import { EditorState, type Extension } from "@codemirror/state";
import { ensureSyntaxTree, syntaxHighlighting, syntaxTree } from "@codemirror/language";
import { classHighlighter, highlightTree } from "@lezer/highlight";
import type { DiffHunk } from "./diff-model";

export type TokenSegment = { text: string; classes?: string };
export type DiffHighlights = Map<string, TokenSegment[]>;
export const tokenHighlighting = syntaxHighlighting(classHighlighter);

type Loader = () => Promise<Extension>;
const cache = new Map<string, Promise<Extension>>();

export function languageName(path: string): string | null {
  const name = path.toLowerCase();
  const extension = name.includes(".") ? name.slice(name.lastIndexOf(".")) : "";
  if ([".js", ".jsx", ".mjs", ".cjs"].includes(extension)) return "javascript";
  if ([".ts", ".tsx", ".mts", ".cts"].includes(extension)) return "typescript";
  if ([".html", ".htm", ".xml", ".svg"].includes(extension)) return "html";
  if (extension === ".css") return "css";
  if (extension === ".json") return "json";
  if ([".md", ".markdown"].includes(extension)) return "markdown";
  if ([".yaml", ".yml"].includes(extension)) return "yaml";
  if (extension === ".py") return "python";
  if (extension === ".rs") return "rust";
  if (extension === ".go") return "go";
  if ([".sh", ".bash", ".zsh"].includes(extension) || ["makefile", "dockerfile"].includes(name)) return "bash";
  return null;
}

const loaders: Record<string, Loader> = {
  javascript: async () => (await import("@codemirror/lang-javascript")).javascript({ jsx: true }),
  typescript: async () => (await import("@codemirror/lang-javascript")).javascript({ jsx: true, typescript: true }),
  html: async () => (await import("@codemirror/lang-html")).html(),
  css: async () => (await import("@codemirror/lang-css")).css(),
  json: async () => (await import("@codemirror/lang-json")).json(),
  markdown: async () => (await import("@codemirror/lang-markdown")).markdown(),
  yaml: async () => (await import("@codemirror/lang-yaml")).yaml(),
  python: async () => (await import("@codemirror/lang-python")).python(),
  rust: async () => (await import("@codemirror/lang-rust")).rust(),
  go: async () => (await import("@codemirror/lang-go")).go(),
  bash: async () => {
    const [{ StreamLanguage }, { shell }] = await Promise.all([
      import("@codemirror/language"),
      import("@codemirror/legacy-modes/mode/shell"),
    ]);
    return StreamLanguage.define(shell);
  },
};

export function loadLanguage(path: string): Promise<Extension> {
  const name = languageName(path);
  if (!name) return Promise.resolve([]);
  let value = cache.get(name);
  if (!value) {
    value = loaders[name]();
    cache.set(name, value);
  }
  return value;
}

export const highlightKey = (hunk: number, side: "old" | "new", line: number) => `${hunk}:${side}:${line}`;

export async function highlightDiff(path: string, hunks: DiffHunk[]): Promise<DiffHighlights> {
  if (!languageName(path)) return new Map();
  const language = await loadLanguage(path);
  const result: DiffHighlights = new Map();
  for (const side of ["old", "new"] as const) {
    const sourceLines: Array<{ key: string; text: string; from: number }> = [];
    let document = "";
    for (const hunk of hunks) {
      hunk.lines.forEach((line, index) => {
        if (line.type === "meta" || (side === "old" ? line.type === "add" : line.type === "del")) return;
        if (document) document += "\n";
        const from = document.length;
        document += line.text;
        sourceLines.push({ key: highlightKey(hunk.index, side, index), text: line.text, from });
      });
    }
    if (!document) continue;
    const state = EditorState.create({ doc: document, extensions: [language] });
    const ranges: Array<{ from: number; to: number; classes: string }> = [];
    const tree = ensureSyntaxTree(state, state.doc.length, 10_000) ?? syntaxTree(state);
    highlightTree(tree, classHighlighter, (from, to, classes) => ranges.push({ from, to, classes }));
    for (const line of sourceLines) {
      const end = line.from + line.text.length;
      const segments: TokenSegment[] = [];
      let position = line.from;
      for (const range of ranges) {
        const from = Math.max(line.from, range.from),
          to = Math.min(end, range.to);
        if (from >= to) continue;
        if (from > position) segments.push({ text: document.slice(position, from) });
        segments.push({ text: document.slice(from, to), classes: range.classes });
        position = to;
      }
      if (position < end) segments.push({ text: document.slice(position, end) });
      result.set(line.key, segments.length ? segments : [{ text: line.text }]);
    }
  }
  return result;
}
