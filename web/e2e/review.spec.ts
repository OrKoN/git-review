import assert from "node:assert/strict";
import puppeteer, { type Browser, type Locator } from "puppeteer";
import { spawn, spawnSync, type ChildProcess } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { createServer } from "node:net";
import { test } from "node:test";
import { runReviewWorkflow } from "./review-workflow.ts";

const binary = (name: string) => resolve(`../bazel-bin/cmd/${name}/${name}_/${name}`);

async function textContent(locator: Locator<Element>) {
  return locator.map((element) => (element.textContent || "").trim()).wait();
}

async function terminate(child: ChildProcess | undefined) {
  if (!child || child.exitCode !== null) return;
  child.kill("SIGTERM");
  await Promise.race([
    new Promise<void>((resolveExit) => child.once("exit", () => resolveExit())),
    new Promise<void>((resolveTimeout) =>
      setTimeout(() => {
        child.kill("SIGKILL");
        resolveTimeout();
      }, 2_000),
    ),
  ]);
}

async function waitForHTTP(url: string) {
  const deadline = Date.now() + 10_000;
  while (Date.now() < deadline) {
    try {
      if ((await fetch(url)).ok) return;
    } catch {
      /* Server is still starting. */
    }
    await new Promise((resolveDelay) => setTimeout(resolveDelay, 25));
  }
  throw new Error(`server did not become ready: ${url}`);
}

test("reviews lines and change blocks", { timeout: 60_000 }, async () => {
  const repo = mkdtempSync(join(tmpdir(), "git-review-e2e-"));
  const otherRepo = mkdtempSync(join(tmpdir(), "git-review-e2e-other-"));
  const runtime = mkdtempSync(join(tmpdir(), "git-review-runtime-"));
  let browser: Browser | undefined;
  const runGit = (cwd: string, ...args: string[]) => {
    const result = spawnSync("git", args, { cwd, encoding: "utf8" });
    assert.equal(result.status, 0, result.stderr);
  };

  runGit(repo, "init", "-q");
  runGit(repo, "config", "user.name", "Review Test");
  runGit(repo, "config", "user.email", "review@example.test");
  writeFileSync(join(repo, "example.go"), 'package example\n\nfunc Old() string { return "old" }\n');
  writeFileSync(join(repo, "clean.go"), "package example\n\nconst Stable = true\n");
  runGit(repo, "add", "example.go", "clean.go");
  runGit(repo, "commit", "-qm", "Initial");
  const reviewLines = [
    `// ${"long-review-context-".repeat(20)}`,
    ...Array.from({ length: 79 }, (_, index) => `// review line ${index + 2}`),
  ];
  writeFileSync(
    join(repo, "example.go"),
    `package example\n\nfunc Improved() string { return "better" }\n${reviewLines.join("\n")}\n`,
  );

  runGit(otherRepo, "init", "-q");
  runGit(otherRepo, "config", "user.name", "Review Test");
  runGit(otherRepo, "config", "user.email", "review@example.test");
  writeFileSync(join(otherRepo, "other.txt"), "before\n");
  runGit(otherRepo, "add", "other.txt");
  runGit(otherRepo, "commit", "-qm", "Initial");
  writeFileSync(join(otherRepo, "other.txt"), "after\n");

  const freePort = () =>
    new Promise<number>((resolvePort, reject) => {
      const listener = createServer();
      listener.once("error", reject);
      listener.listen(0, "127.0.0.1", () => {
        const address = listener.address();
        if (!address || typeof address === "string") return reject(new Error("no test port"));
        listener.close(() => resolvePort(address.port));
      });
    });
  const port = await freePort();
  const tunnelPort = await freePort();
  const hubURL = `http://127.0.0.1:${port}`;
  const environment = {
    ...process.env,
    GIT_REVIEW_HUB_URL: hubURL,
    XDG_RUNTIME_DIR: runtime,
    XDG_CONFIG_HOME: join(runtime, "config"),
    GIT_REVIEW_REPO_SERVER: binary("git-repo-server"),
  };
  const hub = spawn(
    binary("git-review-hub"),
    ["--listen", `127.0.0.1:${port}`, "--tunnel-listen", `127.0.0.1:${tunnelPort}`],
    {
      env: environment,
      stdio: ["ignore", "ignore", "pipe"],
    },
  );
  let agentCLI: ChildProcess | undefined;
  let agentOutput = "";
  let agentErrors = "";
  let attachedOutput = "";
  let attachedCLI: ReturnType<typeof spawn> | undefined;
  let otherRepoCLI: ReturnType<typeof spawn> | undefined;

  try {
    await waitForHTTP(hubURL);
    const enrollment = spawnSync(
      binary("git-review-hub"),
      ["enroll", "--hub-url", hubURL, "--tunnel-address", `127.0.0.1:${tunnelPort}`, "--name", "e2e-agent"],
      { env: environment, encoding: "utf8" },
    );
    assert.equal(enrollment.status, 0, enrollment.stderr);
    const bundle = enrollment.stdout.match(/\n(gr1:[A-Za-z0-9_-]+)\n/)?.[1];
    assert.ok(bundle, enrollment.stdout);
    const enrolled = spawnSync(binary("git-review"), ["enroll"], {
      env: environment,
      encoding: "utf8",
      input: bundle,
    });
    assert.equal(enrolled.status, 0, enrolled.stderr);
    agentCLI = spawn(
      binary("git-review"),
      ["--copy-url=false", "--message", "Improve example API\n\nRename the helper and clarify its result."],
      { cwd: repo, env: environment, stdio: ["ignore", "pipe", "pipe"] },
    );
    agentCLI.stdout?.on("data", (chunk) => {
      agentOutput += chunk.toString();
    });
    agentCLI.stderr?.on("data", (chunk) => {
      agentErrors += chunk.toString();
    });
    const url = await new Promise<string>((resolveURL, reject) => {
      let output = "";
      const timeout = setTimeout(() => reject(new Error(output || "URL not printed")), 10_000);
      agentCLI?.stderr?.on("data", (chunk) => {
        output += chunk.toString();
        const match = output.match(/https?:\/\/[^\s]+/);
        if (match) {
          clearTimeout(timeout);
          resolveURL(match[0]);
        }
      });
      agentCLI?.once("exit", (code) => reject(new Error(`git-review exited ${code}: ${output}`)));
    });
    assert.equal(new URL(url).hash, "", "CLI URL should not contain a repository ID or token");

    browser = await puppeteer.launch({ headless: "shell", pipe: true });
    const page = await browser.newPage();
    await page.setViewport({ width: 1280, height: 800 });
    // The application keeps an EventSource open, so network idle is never reached.
    await page.goto(url.replace("127.0.0.1", "localhost"), { waitUntil: "domcontentloaded" });
    const reviewMessage = page.locator("#review-message-text");
    await page.waitForFunction(() =>
      document.querySelector("#review-message-text")?.textContent?.includes("Improve example API"),
    );
    assert.match(await textContent(reviewMessage), /Improve example API/);
    assert.equal(
      await page.$$eval("#app > .app-header", (elements) => elements.length),
      1,
      "application renders below #app",
    );

    await page.setViewport({ width: 650, height: 800 });
    const pickerLayout = await page
      .locator(".repository-picker")
      .map((picker) => {
        const header = picker.closest("header") as HTMLElement;
        const select = picker.querySelector("select") as HTMLSelectElement;
        return {
          pickerWidth: picker.getBoundingClientRect().width,
          headerWidth: header.getBoundingClientRect().width,
          selectWidth: select.getBoundingClientRect().width,
        };
      })
      .wait();
    assert.ok(pickerLayout.pickerWidth > pickerLayout.headerWidth * 0.85);
    assert.ok(pickerLayout.selectWidth <= pickerLayout.pickerWidth);
    await page.setViewport({ width: 1280, height: 800 });

    const initialRepoID = await page
      .locator<HTMLSelectElement>("#repository-select")
      .map((select) => select.value)
      .wait();
    otherRepoCLI = spawn(binary("git-review"), ["--copy-url=false"], {
      cwd: otherRepo,
      env: environment,
      stdio: ["ignore", "ignore", "pipe"],
    });
    await new Promise<void>((resolveAttached, reject) => {
      let output = "";
      const timeout = setTimeout(() => reject(new Error(output || "second repository did not connect")), 5_000);
      otherRepoCLI?.stderr.on("data", (chunk) => {
        output += chunk.toString();
        if (output.includes(hubURL)) {
          clearTimeout(timeout);
          resolveAttached();
        }
      });
    });
    await page.waitForFunction(() => document.querySelectorAll("#repository-select option").length === 2);
    const otherRepoID = await page
      .locator<HTMLSelectElement>("#repository-select")
      .map((select) => Array.from(select.options).find((option) => option.value !== select.value)?.value || "")
      .wait();
    await page.select("#repository-select", otherRepoID);
    await page.waitForSelector('button.file[data-path="other.txt"]');
    await page.select("#repository-select", initialRepoID);
    await page.waitForSelector('button.file[data-path="example.go"]');

    attachedCLI = spawn(
      binary("git-review"),
      ["--copy-url=false", "--message", "Improve example API\n\nRename the helper and clarify its result."],
      {
        cwd: repo,
        env: environment,
        stdio: ["ignore", "pipe", "pipe"],
      },
    );
    attachedCLI.stdout?.on("data", (chunk) => {
      attachedOutput += chunk.toString();
    });
    await new Promise<void>((resolveAttached, reject) => {
      let output = "";
      const timeout = setTimeout(() => reject(new Error(output || "second CLI did not attach")), 5_000);
      attachedCLI?.stderr.on("data", (chunk) => {
        output += chunk.toString();
        if (output.includes(hubURL)) {
          clearTimeout(timeout);
          resolveAttached();
        }
      });
    });
    const connected = (await fetch(`${hubURL}/api/repositories`).then((response) => response.json())) as unknown[];
    assert.equal(connected.length, 2, "reattachment started a duplicate repository server");
    const conflict = spawnSync(binary("git-review"), ["--copy-url=false", "--message", "Different review"], {
      cwd: repo,
      env: environment,
      encoding: "utf8",
    });
    assert.notEqual(conflict.status, 0);
    assert.match(conflict.stderr, /different proposed message/);

    await runReviewWorkflow({
      page,
      repo,
      agentCLI,
      output: () => ({ agent: agentOutput, attached: attachedOutput, errors: agentErrors }),
    });
    const stopped = spawnSync(binary("git-review"), ["stop"], {
      cwd: repo,
      env: environment,
      encoding: "utf8",
    });
    assert.equal(stopped.status, 0, stopped.stderr);
    assert.match(stopped.stdout, /Please keep the public name stable/);
    await page.waitForFunction(() =>
      document
        .querySelector<HTMLSelectElement>("#repository-select")
        ?.selectedOptions[0]?.textContent?.includes("disconnected"),
    );
  } finally {
    await browser?.close();
    spawnSync(binary("git-review"), ["stop"], { cwd: repo, env: environment, stdio: "ignore", timeout: 5_000 });
    await terminate(agentCLI);
    await terminate(attachedCLI);
    spawnSync(binary("git-review"), ["stop"], {
      cwd: otherRepo,
      env: environment,
      stdio: "ignore",
      timeout: 5_000,
    });
    await terminate(otherRepoCLI);
    await terminate(hub);
    rmSync(repo, { recursive: true, force: true });
    rmSync(otherRepo, { recursive: true, force: true });
    rmSync(runtime, { recursive: true, force: true });
  }
});
