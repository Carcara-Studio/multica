// @vitest-environment node
import { afterEach, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { hierarchyEscalationPageSchema, hierarchyEscalationResponseSchema } from "./hierarchy-escalation-schema";

const id = "11111111-1111-4111-8111-111111111111";
const client = new ApiClient("https://example.test");
afterEach(() => vi.unstubAllGlobals());
it("requires the real creation/replay receipt", () => {
  expect(hierarchyEscalationResponseSchema.parse({ issue_id: id, replayed: true })).toEqual({ issue_id: id, replayed: true });
  expect(hierarchyEscalationResponseSchema.safeParse({ issue_id: id }).success).toBe(false);
  expect(hierarchyEscalationResponseSchema.safeParse({ issue_id: "00000000-0000-0000-0000-000000000000", replayed:false }).success).toBe(false);
});
it.each([{}, [], { items: [], next_cursor: 42 }, { items: [{ decision_issue_id: id }], next_cursor: null }])("refuses malformed history rather than presenting no requests", async (body) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body))));
  await expect(client.listHierarchyEscalations(id)).rejects.toThrow("Could not load escalations");
});
it("preserves empty pages and escapes the pagination cursor", async () => {
  const fetcher = vi.fn().mockResolvedValue(new Response('{"items":[],"next_cursor":null}'));
  vi.stubGlobal("fetch", fetcher);
  await expect(client.listHierarchyEscalations(id, "cursor&injected=1")).resolves.toEqual({ items: [], next_cursor: null });
  expect(String(fetcher.mock.calls[0]?.[0])).toContain("before=cursor%26injected%3D1");
});
it("keeps unknown native decision/repair values visible", () => {
  const item = { request_id:id, source_issue_id:id, source_agent_id:id, source_task_id:id, manager_agent_id:null, human_intervention:true, decision_issue_id:id, decision_status:"future_status", reason:"Reason", context:"", repair:"future_repair" };
  expect(hierarchyEscalationPageSchema.parse({ items:[item], next_cursor:null }).items[0]).toMatchObject(item);
  expect(hierarchyEscalationPageSchema.safeParse({ items:[{...item,manager_agent_id:id}],next_cursor:null }).success).toBe(false);
});
