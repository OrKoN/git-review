import assert from "node:assert/strict";
import { test } from "node:test";
import { highlightDiff, highlightKey, languageName, loadLanguage } from "./language.ts";
import type { DiffHunk } from "./diff-model.ts";

test("maps supported extensions and falls back to plain text", () => {
  assert.equal(languageName("src/component.tsx"), "typescript");
  assert.equal(languageName("config.yaml"), "yaml");
  assert.equal(languageName("Makefile"), "bash");
  assert.equal(languageName("asset.bin"), null);
});

test("caches language chunks", () => {
  assert.equal(loadLanguage("one.go"), loadLanguage("two.go"));
});

test("highlights reconstructed multiline diff streams by source line", async () => {
  const hunks: DiffHunk[] = [
    {
      index: 0,
      header: "@@ -1,2 +1,2 @@",
      lines: [
        { type: "context", raw: " /* comment", text: "/* comment", oldNo: 1, newNo: 1 },
        { type: "context", raw: "  */ const value = 1", text: " */ const value = 1", oldNo: 2, newNo: 2 },
      ],
    },
  ];
  const highlighted = await highlightDiff("example.ts", hunks);
  const second = highlighted.get(highlightKey(0, "new", 1)) || [];
  assert.match(second.map((segment) => segment.text).join(""), /^ \*\/ const value = 1$/);
  assert.ok(second.some((segment) => segment.classes?.includes("tok-comment")));
  assert.ok(second.some((segment) => segment.classes?.includes("tok-keyword")));
});

test("highlights syntax beyond the first 100 diff lines", async () => {
  const lines = Array.from({ length: 150 }, (_, index) => ({
    type: "context" as const,
    raw: ` const value${index} = ${index}`,
    text: `const value${index} = ${index}`,
    oldNo: index + 1,
    newNo: index + 1,
  }));
  const highlighted = await highlightDiff("example.ts", [{ index: 0, header: "@@ -1,150 +1,150 @@", lines }]);
  const last = highlighted.get(highlightKey(0, "new", 149)) || [];

  assert.equal(last.map((segment) => segment.text).join(""), "const value149 = 149");
  assert.ok(last.some((segment) => segment.text === "const" && segment.classes?.includes("tok-keyword")));
});

test("unsupported files remain unhighlighted", async () => {
  assert.equal((await highlightDiff("data.unknown", [])).size, 0);
});
