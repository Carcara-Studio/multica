// @vitest-environment node
import { describe, expect, it } from "vitest";
import { hierarchyDelegationResponseSchema } from "./hierarchy-delegation-schema";
import { parseWithFallback } from "./schema";

describe("hierarchy delegation receipt", () => {
  const id = "11111111-1111-4111-8111-111111111111";
  it("preserves creation and replay identities", () => {
    expect(hierarchyDelegationResponseSchema.parse({ issue_id: id })).toEqual({
      issue_id: id,
      replayed: false,
    });
    expect(
      hierarchyDelegationResponseSchema.parse({
        issue_id: id,
        replayed: true,
        future_field: "ignored",
      }),
    ).toEqual({ issue_id: id, replayed: true });
  });
  it.each([null, {}, { issue_id: "invalid" }, { issue_id: id, replayed: "true" }])(
    "rejects malformed receipts without inventing an Issue",
    (value) => {
      expect(
        parseWithFallback(value, hierarchyDelegationResponseSchema, null, {
          endpoint: "POST /api/issues/:id/hierarchy-delegations",
        }),
      ).toBeNull();
    },
  );
});
