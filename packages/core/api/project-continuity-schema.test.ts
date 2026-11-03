// @vitest-environment node
import { afterEach, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { parseContinuity, projectContinuitySchema, projectContinuityDraftSchema, validContinuityContent } from "./project-continuity-schema";
import { setSchemaLogger } from "./schema";
import { noopLogger } from "../logger";

const id = "11111111-1111-4111-8111-111111111111";
const revision = "22222222-2222-4222-8222-222222222222";
const empty = { project_id: id, revision: "", content: "", reviewed_by: "", reviewed_at: "" };
const saved = { ...empty, revision, content: "Keep opaque IDs.", reviewed_by: id, reviewed_at: "2026-11-03T12:00:00Z" };
const client = new ApiClient("https://example.test");
afterEach(() => vi.unstubAllGlobals());

it("validates UTF-8 and the Go JSON encoding budget, including HTML escaping", () => {
  expect(validContinuityContent("é".repeat(2000))).toBe(true);
  expect(validContinuityContent("é".repeat(2001))).toBe(false);
  expect(validContinuityContent("<".repeat(1000))).toBe(false);
  expect(validContinuityContent("x\0")).toBe(false);
  expect(projectContinuitySchema.safeParse(empty).success).toBe(true);
  expect(projectContinuitySchema.safeParse({ ...empty, revision, reviewed_by: id, reviewed_at: saved.reviewed_at }).success).toBe(true);
});

it("rejects malformed and foreign receipts without logging raw memory", async () => {
  const warn = vi.fn();
  setSchemaLogger({ ...noopLogger, warn });
  try {
    for (const body of [{}, { ...empty, content: "private-memory" }, { ...saved, project_id: revision }, { ...saved, reviewed_at: "invalid" }]) {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body))));
      await expect(client.getProjectContinuity(id)).rejects.toThrow();
    }
    expect(warn).not.toHaveBeenCalled();
  } finally { setSchemaLogger(noopLogger); }
});

it("consults drafts without writing and refuses missing source provenance", async () => {
  const draft = { version: 1, available: false, reason: "no_eligible_completed_task", current: empty, source: null, suggested_content: null };
  const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify(draft)));
  vi.stubGlobal("fetch", fetcher);
  await expect(client.getProjectContinuityDraft(id)).resolves.toEqual(draft);
  expect(String(fetcher.mock.calls[0]?.[0])).toContain(`/projects/${id}/continuity/draft`);
  expect(fetcher.mock.calls[0]?.[1]?.method ?? "GET").toBe("GET");
  expect(() => parseContinuity({ ...draft, available: true }, projectContinuityDraftSchema, id)).toThrow();
});

it("publishes the exact revision once and requires a new matching receipt", async () => {
  const fetcher = vi.fn().mockResolvedValueOnce(new Response(JSON.stringify(saved)))
    .mockResolvedValueOnce(new Response(JSON.stringify({ ...saved, content: "wrong" })))
    .mockResolvedValueOnce(new Response(JSON.stringify(saved)))
    .mockResolvedValueOnce(new Response(JSON.stringify({ error: "private-body" }), { status: 409 }));
  vi.stubGlobal("fetch", fetcher);
  await expect(client.publishProjectContinuity(id, { expected_revision: "", content: saved.content })).resolves.toEqual(saved);
  expect(JSON.parse(String(fetcher.mock.calls[0]?.[1]?.body))).toEqual({ expected_revision: "", content: saved.content });
  await expect(client.publishProjectContinuity(id, { expected_revision: "", content: saved.content })).rejects.toThrow();
  await expect(client.publishProjectContinuity(id, { expected_revision: revision, content: saved.content })).rejects.toThrow();
  await expect(client.publishProjectContinuity(id, { expected_revision: revision, content: saved.content })).rejects.toMatchObject({ status: 409 });
  expect(fetcher).toHaveBeenCalledTimes(4);
});
