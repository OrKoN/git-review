import assert from "node:assert/strict";
import { test } from "node:test";
import { buildFileTree } from "./file-tree.ts";

test("builds repository files into a sorted directory tree", () => {
  const tree = buildFileTree([
    { path: "web/src/z.ts", kind: "file" },
    { path: "README.md", kind: "file" },
    { path: "internal/server/server.go", kind: "file" },
    { path: "web/index.html", kind: "file" },
    { path: "web/src/app.ts", kind: "file" },
  ]);

  assert.deepEqual(
    tree.files.map((file) => file.path),
    ["README.md"],
  );
  assert.deepEqual(
    tree.directories.map((directory) => directory.name),
    ["internal", "web"],
  );
  assert.deepEqual(
    tree.directories[1].files.map((file) => file.path),
    ["web/index.html"],
  );
  assert.deepEqual(
    tree.directories[1].directories[0].files.map((file) => file.path),
    ["web/src/app.ts", "web/src/z.ts"],
  );
});
