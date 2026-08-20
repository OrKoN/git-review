import { execFileSync } from "node:child_process";
import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const output = join(root, "internal/licenses/THIRD_PARTY_LICENSES.txt");
const check = process.argv.includes("--check");
const licensePattern = /^(licen[cs]e|copying|notice|patents)(\..*)?$/i;

function bazelRepositoryRoot(name) {
  const location = execFileSync(
    "bazel",
    ["query", `@${name}//:all`, "--output=location"],
    { cwd: root, encoding: "utf8", stdio: ["ignore", "pipe", "inherit"] },
  ).split("\n").find(Boolean);
  if (!location) throw new Error(`Bazel repository ${name} has no targets`);
  return dirname(location.replace(/:\d+:\d+:.*$/, ""));
}

function legalFiles(directory) {
  const names = readdirSync(directory).filter((name) => licensePattern.test(name)).sort();
  if (names.length === 0) throw new Error(`no license or notice file in ${directory}`);
  return names.map((name) => ({ name, text: readFileSync(join(directory, name), "utf8").trim() }));
}

function goRepository(module) {
  if (module.startsWith("github.com/")) return `com_github_${module.slice(11).replaceAll("/", "_").replaceAll("-", "_")}`;
  if (module.startsWith("golang.org/x/")) return `@gazelle++go_deps+org_golang_x_${module.slice(13).replaceAll("/", "_").replaceAll("-", "_")}`;
  throw new Error(`add a Bazel repository mapping for ${module}`);
}

function goComponents() {
  const source = readFileSync(join(root, "go.mod"), "utf8");
  const modules = [...source.matchAll(/^[ \t]*(?:require[ \t]+)?((?:github\.com|golang\.org)\/\S+)[ \t]+(v\S+)/gm)]
    .map(([, name, version]) => ({ name, version }));
  const moduleSource = readFileSync(join(root, "MODULE.bazel"), "utf8");
  const goVersion = moduleSource.match(/go_sdk\.download\([\s\S]*?version = "([^"]+)"/)[1];
  return [{
    name: "The Go standard library",
    version: goVersion,
    homepage: "https://go.dev/",
    license: legalFiles(bazelRepositoryRoot("go_sdk")),
  }, ...modules.map(({ name, version }) => ({
    name,
    version,
    homepage: `https://${name}`,
    license: legalFiles(bazelRepositoryRoot(goRepository(name))),
  }))];
}

function repositoryURL(value) {
  const raw = typeof value === "string" ? value : value?.url;
  return raw?.replace(/^git\+/, "").replace(/\.git$/, "") ?? "(not declared)";
}

function npmComponents() {
  const lock = JSON.parse(readFileSync(join(root, "web/package-lock.json"), "utf8"));
  return Object.entries(lock.packages)
    .filter(([path, metadata]) => path.startsWith("node_modules/") && metadata.dev !== true)
    .map(([path, metadata]) => {
      const directory = join(root, "web", path);
      const manifest = JSON.parse(readFileSync(join(directory, "package.json"), "utf8"));
      return {
        name: manifest.name,
        version: metadata.version,
        homepage: manifest.homepage ?? repositoryURL(manifest.repository),
        license: legalFiles(directory),
      };
    });
}

function render(components) {
  const separator = "=".repeat(78);
  const sections = components
    .sort((a, b) => a.name.localeCompare(b.name))
    .map((component) => {
      const files = component.license.map(({ name, text }) => `--- ${name} ---\n\n${text}`).join("\n\n");
      return `Component: ${component.name}\nVersion: ${component.version}\nHomepage: ${component.homepage}\n\n${files}`;
    });
  return `THIRD-PARTY SOFTWARE NOTICES

git-review includes the following third-party software. These notices apply
to those components and do not change the license of git-review itself.

${sections.join(`\n\n${separator}\n\n`)}\n`;
}

if (check) {
  const notices = readFileSync(output, "utf8");
  const actual = new Map([...notices.matchAll(/^Component: (.+)\nVersion: (.+)$/gm)].map((match) => [match[1], match[2]]));
  const goSource = readFileSync(join(root, "go.mod"), "utf8");
  const expected = new Map([...goSource.matchAll(/^[ \t]*(?:require[ \t]+)?((?:github\.com|golang\.org)\/\S+)[ \t]+(v\S+)/gm)]
    .map((match) => [match[1], match[2]]));
  const moduleSource = readFileSync(join(root, "MODULE.bazel"), "utf8");
  expected.set("The Go standard library", moduleSource.match(/go_sdk\.download\([\s\S]*?version = "([^"]+)"/)[1]);
  const lock = JSON.parse(readFileSync(join(root, "web/package-lock.json"), "utf8"));
  for (const [path, metadata] of Object.entries(lock.packages)) {
    if (path.startsWith("node_modules/") && metadata.dev !== true) {
      expected.set(path.slice(path.lastIndexOf("node_modules/") + 13), metadata.version);
    }
  }
  if (JSON.stringify([...actual].sort()) !== JSON.stringify([...expected].sort())) {
    throw new Error("third-party notices are stale; run npm --prefix web run licenses");
  }
} else {
  writeFileSync(output, render([...goComponents(), ...npmComponents()]));
}
