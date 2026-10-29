import { z } from "zod";

const uuid = z.string().uuid().refine((value) => value !== "00000000-0000-0000-0000-000000000000");
const revisionSchema = z.object({
  generation: uuid.or(z.literal("")),
  primary_generation: uuid.or(z.literal("")),
  state_version: z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER),
}).refine((revision) => revision.state_version === 0
  ? revision.generation === "" && revision.primary_generation === ""
  : revision.primary_generation !== "");

export const agentFallbackSchema = z.object({
  configured: z.boolean(),
  execution_enabled: z.literal(false).default(false),
  revision: revisionSchema,
  configuration: z.object({
    agent_id: uuid,
    runtime_id: uuid,
    mode: z.enum(["native_login_path", "strict", "unknown"]).catch("unknown"),
    model: z.string(),
    thinking_level: z.string().default(""),
    service_tier: z.string().default(""),
    environment_keys: z.array(z.string()).default([]),
    argument_count: z.number().int().nonnegative().default(0),
    has_mcp_config: z.boolean().default(false),
    has_runtime_config: z.boolean().default(false),
  }).nullable(),
}).refine((reply) => reply.configured
  ? reply.configuration !== null && reply.revision.generation !== "" && reply.revision.state_version > 0
  : reply.configuration === null && reply.revision.generation === "");

export type AgentFallbackResponse = z.infer<typeof agentFallbackSchema>;
export type FallbackRevision = AgentFallbackResponse["revision"];
export interface SaveAgentFallbackRequest {
  mode: "native_login_path";
  runtime_id: string;
  model: string;
  thinking_level?: string;
  service_tier?: string;
  expected_revision: FallbackRevision;
  overrides: {
    version: 1;
    custom_env: Record<string, string>;
    custom_args: string[];
    runtime_config: Record<string, unknown>;
    mcp_config: Record<string, unknown> | null;
  };
}
