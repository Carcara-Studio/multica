import { z } from "zod";
import { parseWithFallback } from "./schema";

const uuid = z.string().uuid().refine((s) => s !== "00000000-0000-0000-0000-000000000000");
const bytes = (s: string) => new TextEncoder().encode(s).length;

export function validContinuityContent(s: string): boolean {
  return s.trim().length > 0 && !s.includes("\0") && bytes(s) <= 4000
    && bytes(JSON.stringify(s).replace(/[<>&\u2028\u2029]/g, (c) => `\\u${c.charCodeAt(0).toString(16).padStart(4, "0")}`)) <= 6000;
}

export const projectContinuitySchema = z.object({
  project_id: uuid,
  revision: uuid.or(z.literal("")),
  content: z.string(),
  reviewed_by: uuid.or(z.literal("")),
  reviewed_at: z.string(),
}).refine((c) => c.revision === ""
  ? c.content === "" && c.reviewed_by === "" && c.reviewed_at === ""
  : uuid.safeParse(c.reviewed_by).success && !Number.isNaN(Date.parse(c.reviewed_at))
    && (c.content === "" || validContinuityContent(c.content)));

export const projectContinuityDraftSchema = z.object({
  version: z.literal(1),
  available: z.boolean(),
  reason: z.string().min(1),
  current: projectContinuitySchema,
  source: z.object({
    task_id: uuid, issue_id: uuid, issue_revision: z.number().int().positive(),
    issue_status: z.string(), completed_at: z.string().refine((s) => !Number.isNaN(Date.parse(s))),
    sha256: z.string().regex(/^[a-f0-9]{64}$/), state: z.literal("unreviewed"),
    report_excerpt: z.string().refine((s) => s.trim().length > 0 && bytes(s) <= 1200),
    truncated: z.boolean(),
  }).nullable(),
  suggested_content: z.string().refine(validContinuityContent).nullable(),
}).refine((d) => d.available ? d.source !== null : d.source === null && d.suggested_content === null);

export type ProjectContinuity = z.infer<typeof projectContinuitySchema>;
export type ProjectContinuityDraft = z.infer<typeof projectContinuityDraftSchema>;

export function parseContinuity<T extends ProjectContinuity | ProjectContinuityDraft>(raw: unknown, schema: z.ZodType<T>, projectId: string): T {
  // Validate before the shared fallback logger: malformed memory must not enter logs.
  const checked = schema.safeParse(raw);
  if (!checked.success) throw new Error("Invalid project continuity response");
  const parsed = parseWithFallback<T | null>(checked.data, schema, null, { endpoint: "projectContinuity" });
  if (parsed === null) throw new Error("Invalid project continuity response");
  const scope = "current" in parsed ? parsed.current.project_id : parsed.project_id;
  if (scope !== projectId) throw new Error("Project continuity scope mismatch");
  return parsed;
}
