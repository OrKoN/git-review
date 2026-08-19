import { cp, mkdir, readFile, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { build } from "esbuild";

const root = resolve(import.meta.dirname);
const output = resolve(process.argv[2] || resolve(root, "dist"));
await mkdir(resolve(output, "assets"), { recursive: true });
await build({
  entryPoints: [resolve(root, "src/app.ts")],
  bundle: true,
  minify: true,
  sourcemap: false,
  outfile: resolve(output, "assets/app.js"),
  platform: "browser",
  format: "esm",
});
const html = (await readFile(resolve(root, "index.html"), "utf8"))
  .replace("/src/style.css", "/assets/style.css")
  .replace("/src/app.ts", "/assets/app.js");
await writeFile(resolve(output, "index.html"), html);
await cp(resolve(root, "src/style.css"), resolve(output, "assets/style.css"));
