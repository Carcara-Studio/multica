// @vitest-environment node
import { afterEach, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { agentFallbackSchema, type SaveAgentFallbackRequest } from "./agent-fallback-schema";

const id = "11111111-1111-4111-8111-111111111111";
const revision = { generation: id, primary_generation: id, state_version: 2 };
const empty = { configured: false, execution_enabled: false, revision: { generation: "", primary_generation: "", state_version: 0 }, configuration: null };
const saved = { configured: true, execution_enabled: false, revision, configuration: { agent_id: id, runtime_id: id, mode: "native_login_path", model: "model-test" } };
const client = new ApiClient("https://example.test");
afterEach(() => vi.unstubAllGlobals());

it("keeps state-only revisions for recreation and strips unknown secret fields", () => {
  expect(agentFallbackSchema.parse({ ...empty, revision: { ...revision, generation: "" } }).revision.state_version).toBe(2);
  const reply = agentFallbackSchema.parse({ ...saved, configuration: { ...saved.configuration, mode: "future_mode", custom_env: { KEY: "must-not-reach-consumer" } } });
  expect(reply.configuration?.mode).toBe("unknown");
  expect(reply.configuration).not.toHaveProperty("custom_env");
  expect(reply.configuration?.environment_keys).toEqual([]);
});

it.each([{}, [], { ...empty, execution_enabled: true }, { ...saved, configuration: null }, { ...empty, revision }, { ...saved, revision: { ...revision, state_version: 2 ** 54 } }])("refuses malformed or enabled responses without reporting save success", async (body) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body))));
  await expect(client.getAgentFallback(id)).rejects.toThrow("Could not load fallback");
});

it("writes explicit native intent with CAS and requires a configured receipt", async () => {
  const data: SaveAgentFallbackRequest = { mode: "native_login_path", runtime_id: id, model: "model-test", expected_revision: empty.revision, overrides: { version: 1, custom_env: {}, custom_args: [], runtime_config: {}, mcp_config: null } };
  const fetcher = vi.fn()
    .mockResolvedValueOnce(new Response(JSON.stringify(saved)))
    .mockResolvedValueOnce(new Response(JSON.stringify(empty)))
    .mockResolvedValueOnce(new Response(JSON.stringify({ ...saved, configuration: { ...saved.configuration, mode: "strict" } })));
  vi.stubGlobal("fetch", fetcher);
  await expect(client.saveAgentFallback(id, data)).resolves.toMatchObject({ configured: true, execution_enabled: false });
  expect(JSON.parse(String(fetcher.mock.calls[0]?.[1]?.body))).toEqual(data);
  await expect(client.saveAgentFallback(id, data)).rejects.toThrow("Could not confirm fallback");
  await expect(client.saveAgentFallback(id, data)).rejects.toThrow("Could not confirm fallback");
});

it("deletes with the exact revision and escapes resource identifiers", async () => {
  const fetcher = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
  vi.stubGlobal("fetch", fetcher);
  await client.deleteAgentFallback("agent/other", revision);
  expect(String(fetcher.mock.calls[0]?.[0])).toContain("/agents/agent%2Fother/fallback");
  expect(fetcher.mock.calls[0]?.[1]?.method).toBe("DELETE");
  expect(JSON.parse(String(fetcher.mock.calls[0]?.[1]?.body))).toEqual({ expected_revision: revision });
});
