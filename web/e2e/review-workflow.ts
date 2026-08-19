import assert from "node:assert/strict";
import { writeFileSync } from "node:fs";
import { join } from "node:path";
import type { ChildProcess } from "node:child_process";
import type { Locator, Page } from "puppeteer";

async function textContent(locator: Locator<Element>) {
  return locator.map((element) => (element.textContent || "").trim()).wait();
}

interface ReviewWorkflow {
  page: Page;
  repo: string;
  agentCLI: ChildProcess | undefined;
  output(): { agent: string; attached: string; errors: string };
}

export async function runReviewWorkflow({ page, repo, agentCLI, output }: ReviewWorkflow) {
  await page.locator('button.file[data-path="example.go"][data-area="unstaged"]').click();
  await page.locator(".diff-row.add .diff-code").wait();
  await page.locator(".diff-code .tok-keyword").wait();
  assert.ok(
    (await page
      .locator(".diff-row")
      .map((row) => row.getBoundingClientRect().height)
      .wait()) <= 24,
    "unified diff rows should not create anonymous whitespace grid rows",
  );
  const sectionTitles = await page
    .locator(".file-section-heading")
    .map((heading) => heading.textContent || "")
    .wait();
  assert.match(sectionTitles.trimStart(), /^Unstaged/);
  assert.equal(await textContent(page.locator("#diff-summary")), "+81−11 hunk");
  assert.equal(await textContent(page.locator("#hunk-position")), "Hunk 1 of 1");

  await page.locator(".diff-row.add .diff-code").click();
  const commentEditor = page.locator(".inline-comment-editor");
  await commentEditor.wait();
  await page.locator(".inline-comment-editor textarea").fill("Please keep the public name stable.");
  await page.locator(".inline-comment-actions .primary").click();
  await page.locator(".inline-thread").wait();
  await new Promise<void>((resolveComment, reject) => {
    const deadline = Date.now() + 5_000;
    const check = () =>
      output().agent.includes("Please keep the public name stable.")
        ? resolveComment()
        : Date.now() > deadline
          ? reject(
              new Error(
                `comment did not reach agent stream; stdout=${JSON.stringify(output().agent)} attached=${JSON.stringify(output().attached)} stderr=${JSON.stringify(output().errors)} exit=${agentCLI?.exitCode}`,
              ),
            )
          : setTimeout(check, 25);
    check();
  });

  await page.locator("#subject").fill("Draft survives refresh");
  await page.locator("#body").fill("Still writing this description.");
  await page.locator("#comment-file").click();
  await page.locator(".inline-comment-editor textarea").fill("Draft review feedback");
  const scrollBefore = await page.evaluate(() => {
    const workspace = document.querySelector<HTMLElement>("#workspace")!;
    workspace.scrollTop = 420;
    return { workspaceTop: workspace.scrollTop };
  });
  assert.ok(scrollBefore.workspaceTop > 0);
  writeFileSync(join(repo, "background-update.txt"), "external update\n");
  await page.waitForSelector('button.file[data-path="background-update.txt"]');
  const scrollAfter = await page.evaluate(() => ({
    workspaceTop: document.querySelector<HTMLElement>("#workspace")!.scrollTop,
  }));
  assert.ok(
    Math.abs(scrollAfter.workspaceTop - scrollBefore.workspaceTop) <= 2,
    `workspace scroll changed from ${scrollBefore.workspaceTop} to ${scrollAfter.workspaceTop}`,
  );
  assert.equal(
    await page
      .locator<HTMLInputElement>("#subject")
      .map((input) => input.value)
      .wait(),
    "Draft survives refresh",
  );
  assert.equal(
    await page
      .locator<HTMLTextAreaElement>("#body")
      .map((input) => input.value)
      .wait(),
    "Still writing this description.",
  );
  assert.equal(
    await page
      .locator<HTMLTextAreaElement>(".inline-comment-editor textarea")
      .map((input) => input.value)
      .wait(),
    "Draft review feedback",
  );
  await page.locator('[data-action="cancel-comment"]').click();
  await page.evaluate(() => {
    document.querySelector<HTMLElement>("#workspace")!.scrollTop = 0;
  });

  await page.locator("#edit").click();
  await page.locator("#editor .cm-editor").wait();
  await page.locator("#cancel-edit").click();
  await page.locator("#edit").click();
  await page.locator("#editor .cm-editor").wait();
  assert.equal(
    await page
      .locator("#editor .cm-content")
      .map((element) => element.getAttribute("contenteditable"))
      .wait(),
    "true",
  );
  await page.locator("#editor .cm-content").click();
  await page.keyboard.down("Control");
  await page.keyboard.press("End");
  await page.keyboard.up("Control");
  await page.keyboard.type("// browser edit");
  assert.match(await textContent(page.locator("#editor .cm-content")), /browser edit/);
  await page.locator("#save").click();
  await page.locator(".diff-row.add .diff-code").wait();

  await page.locator("#stage").click();
  await page.waitForSelector('button.file.active[data-path="background-update.txt"][data-area="unstaged"]');
  await page.locator('button.file[data-path="example.go"][data-area="staged"]').click();
  await page.locator(".diff-row.add .diff-code").wait();
  assert.equal(await textContent(page.locator("#stage")), "Unstage file");

  await page.locator("#stage").click();
  await page.waitForSelector('button.file.active[data-path="example.go"][data-area="unstaged"]');
  await page.locator(".diff-row.add .diff-code").wait();
  assert.equal(await textContent(page.locator("#stage")), "Stage file");

  await page.locator("#view-toggle").click();
  await page.locator(".block-button.apply").wait();
  await page.locator(".block-button.reject").wait();
  const splitLayout = await page
    .locator(".split-cell:not(.empty)")
    .map((cell) => {
      const number = cell.querySelector(".line-number") as HTMLElement;
      const code = cell.querySelector(".diff-code") as HTMLElement;
      const cellRect = cell.getBoundingClientRect();
      const numberRect = number.getBoundingClientRect();
      const codeRect = code.getBoundingClientRect();
      return {
        codeStartsAfterNumber: codeRect.left >= numberRect.right,
        codeStaysInsideCell: codeRect.right <= cellRect.right,
        overflow: getComputedStyle(cell).overflow,
        whiteSpace: getComputedStyle(code).whiteSpace,
      };
    })
    .wait();
  assert.equal(splitLayout.codeStartsAfterNumber, true);
  assert.equal(splitLayout.codeStaysInsideCell, true);
  assert.equal(splitLayout.overflow, "visible");
  assert.equal(splitLayout.whiteSpace, "pre-wrap");
  const splitWidths = await page
    .locator(".split-row")
    .map((row) => {
      const cells = row.querySelectorAll<HTMLElement>(":scope > .split-cell");
      return [cells[0].getBoundingClientRect().width, cells[1].getBoundingClientRect().width];
    })
    .wait();
  assert.ok(Math.abs(splitWidths[0] - splitWidths[1]) <= 2, `split panes are unbalanced: ${splitWidths.join(", ")}`);

  await page.locator('[data-test-id="files-tab"]').click();
  await page.locator('[data-test-id="file-filter"]').fill("clean.go");
  await page.locator('[data-test-id="repository-file"][data-path="clean.go"]').click();
  await page.locator('[data-test-id="source-viewer"] .cm-editor').wait();
  const sourceLayout = await page
    .locator("#editor .cm-scroller")
    .map((scroller) => {
      const gutters = scroller.querySelector<HTMLElement>(".cm-gutters")!;
      const content = scroller.querySelector<HTMLElement>(".cm-content")!;
      return {
        display: getComputedStyle(scroller).display,
        text: content.innerText,
        gutterRight: gutters.getBoundingClientRect().right,
        contentLeft: content.getBoundingClientRect().left,
        contentWidth: content.getBoundingClientRect().width,
      };
    })
    .wait();
  assert.equal(sourceLayout.display, "flex", JSON.stringify(sourceLayout));
  assert.match(sourceLayout.text, /const Stable = true/, JSON.stringify(sourceLayout));
  assert.ok(sourceLayout.contentLeft >= sourceLayout.gutterRight, JSON.stringify(sourceLayout));
  assert.ok(sourceLayout.contentWidth > 100, JSON.stringify(sourceLayout));
  assert.equal(
    await page
      .locator("#editor .cm-content")
      .map((element) => element.getAttribute("contenteditable"))
      .wait(),
    "false",
  );
  await page.evaluate(() => {
    document
      .querySelector<HTMLElement>(".cm-comment-gutter .cm-gutterElement:has(.source-comment-add)")!
      .dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
  });
  await page.locator(".inline-comment-editor textarea").fill("Clean source comment");
  await page.locator(".inline-comment-actions .primary").click();
  await page.locator(".source-comment-count").wait();

  await page.locator('[data-test-id="changes-tab"]').click();
  await page.locator('[data-test-id="stage-all"]').click();
  await page.waitForSelector('[data-test-id="commit-form"] button:not([disabled])');
  await page.screenshot({ path: "/tmp/git-review-diff-ui.png", fullPage: true });
}
