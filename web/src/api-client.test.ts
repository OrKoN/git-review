import assert from "node:assert/strict";
import { describe, it, test } from "node:test";
import { parseSSE, RepositoryClient } from "./api-client.ts";

describe("parseSSE", () => {
  it("extracts named events and ignores comments", () => {
    const parsed = parseSSE(": connected\n\nevent: repository\ndata: {}\n\n");
    assert.deepEqual(parsed.events, ["repository"]);
    assert.equal(parsed.remainder, "");
  });

  it("retains an incomplete frame for the next chunk", () => {
    const first = parseSSE("event: comments\nda");
    assert.deepEqual(first.events, []);
    const second = parseSSE(first.remainder + "ta: {}\r\n\r\n");
    assert.deepEqual(second.events, ["comments"]);
    assert.equal(second.remainder, "");
  });
});

test("repository requests stay same-origin and carry no browser token", async () => {
  const originalFetch = globalThis.fetch;
  let requested = "";
  let authorization: string | null = "unexpected";
  globalThis.fetch = (input, options) => {
    requested = String(input);
    authorization = new Headers(options?.headers).get("Authorization");
    return Promise.resolve(
      new Response('{"ok":true}', { status: 200, headers: { "Content-Type": "application/json" } }),
    );
  };
  try {
    const client = new RepositoryClient({ baseUrl: "/api/repositories/repo/proxy" });
    assert.deepEqual(await client.request("/api/repository"), { ok: true });
    assert.equal(requested, "/api/repositories/repo/proxy/api/repository");
    assert.equal(authorization, null);
  } finally {
    globalThis.fetch = originalFetch;
  }
});
