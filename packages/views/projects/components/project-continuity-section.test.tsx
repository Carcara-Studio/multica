import { act, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, expect, it, vi } from "vitest";
import { ApiError } from "@multica/core/api";
import { continuityKey } from "@multica/core/projects/continuity";
import { renderWithI18n } from "../../test/i18n";
import { ProjectContinuitySection } from "./project-continuity-section";

const mocks = vi.hoisted(() => ({
  wsId: "workspace-1", api: { getProjectContinuity: vi.fn(), getProjectContinuityDraft: vi.fn(), publishProjectContinuity: vi.fn() },
}));
vi.mock("@multica/core/api", async (original) => ({ ...await original<typeof import("@multica/core/api")>(), api: mocks.api }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => mocks.wsId }));
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ issueDetail: (id: string) => `/work/issues/${id}` }) }));
vi.mock("../../navigation", () => ({ AppLink: (props: React.ComponentProps<"a">) => <a {...props} /> }));

const id = "11111111-1111-4111-8111-111111111111";
const revision = "22222222-2222-4222-8222-222222222222";
const current = { project_id: id, revision, content: "Published decision", reviewed_by: id, reviewed_at: "2026-11-03T12:00:00Z" };
const source = { task_id: id, issue_id: id, issue_revision: 1, issue_status: "in_review", completed_at: current.reviewed_at, sha256: "a".repeat(64), state: "unreviewed", report_excerpt: "Unverified task result", truncated: false };
const draft = { version: 1, available: true, reason: "review_required", current, source, suggested_content: "Suggested memory" };

beforeEach(() => {
  vi.resetAllMocks(); mocks.wsId = "workspace-1";
  mocks.api.getProjectContinuity.mockResolvedValue(current);
  mocks.api.getProjectContinuityDraft.mockResolvedValue(draft);
  mocks.api.publishProjectContinuity.mockImplementation(async (_id, data) => ({ ...current, content: data.content, revision: id }));
});

function mount() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const view = renderWithI18n(<QueryClientProvider client={qc}><ProjectContinuitySection projectId={id} /></QueryClientProvider>);
  return { qc, ...view };
}
async function openEditor() {
  await userEvent.click(await screen.findByRole("button", { name: "Review memory" }));
  return screen.getByRole("textbox", { name: "Memory content" });
}

it("requires explicit suggestion loading and publication before updating the current memory", async () => {
  mount(); await openEditor();
  await userEvent.click(screen.getByRole("button", { name: "Consult latest task report" }));
  expect(await screen.findByText(/Unreviewed report/)).toBeVisible();
  expect(screen.getByRole("link", { name: "Open source task" })).toHaveAttribute("href", `/work/issues/${id}`);
  expect(screen.getByRole("textbox")).toHaveValue(current.content);
  expect(mocks.api.publishProjectContinuity).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("button", { name: "Use suggestion" }));
  await userEvent.click(screen.getByRole("button", { name: "Publish memory" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(mocks.api.publishProjectContinuity).toHaveBeenCalledWith(id, { expected_revision: revision, content: "Suggested memory" });
  expect(screen.getByText("Suggested memory")).toBeVisible();
});

it("preserves local edits across draft consultation and background refetches", async () => {
  const { qc } = mount(); const field = await openEditor();
  await userEvent.clear(field); await userEvent.type(field, "Local revision");
  await userEvent.click(screen.getByRole("button", { name: "Consult latest task report" }));
  expect(await screen.findByRole("button", { name: "Use suggestion" })).toBeDisabled();
  act(() => qc.setQueryData(continuityKey(mocks.wsId, id), { ...current, revision: id, content: "Other publication" }));
  expect(field).toHaveValue("Local revision");
  await userEvent.click(screen.getByRole("button", { name: "Publish memory" }));
  expect(mocks.api.publishProjectContinuity).toHaveBeenCalledWith(id, { expected_revision: revision, content: "Local revision" });
});

it("keeps edits on conflict and requires explicit discard/reload before a new publication", async () => {
  mocks.api.publishProjectContinuity.mockRejectedValueOnce(new ApiError("raw-private-body", 409, "Conflict"));
  mount(); const field = await openEditor();
  await userEvent.clear(field); await userEvent.type(field, "Local revision");
  await userEvent.click(screen.getByRole("button", { name: "Publish memory" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("published memory changed");
  expect(field).toHaveValue("Local revision");
  expect(screen.getByRole("button", { name: "Publish memory" })).toBeDisabled();
  expect(screen.queryByText("raw-private-body")).not.toBeInTheDocument();
  mocks.api.getProjectContinuity.mockResolvedValue({ ...current, revision: id, content: "Other publication" });
  await userEvent.click(screen.getByRole("button", { name: "Discard edits and load published version" }));
  await waitFor(() => expect(field).toHaveValue("Other publication"));
  await userEvent.type(field, " reviewed");
  await userEvent.click(screen.getByRole("button", { name: "Publish memory" }));
  expect(mocks.api.publishProjectContinuity).toHaveBeenLastCalledWith(id, { expected_revision: id, content: "Other publication reviewed" });
  expect(mocks.api.publishProjectContinuity).toHaveBeenCalledTimes(2);
});

it("preserves the editor when publication fails without claiming success", async () => {
  mocks.api.publishProjectContinuity.mockRejectedValue(new Error("private-error"));
  mount(); const field = await openEditor(); await userEvent.type(field, " updated");
  await userEvent.click(screen.getByRole("button", { name: "Publish memory" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Publication could not be confirmed");
  expect(field).toHaveValue("Published decision updated");
  expect(screen.getByRole("dialog")).toBeVisible();
  expect(mocks.api.publishProjectContinuity).toHaveBeenCalledTimes(1);
});

it("shows no eligible report without manufacturing a suggestion", async () => {
  mocks.api.getProjectContinuityDraft.mockResolvedValue({ ...draft, available: false, reason: "no_eligible_completed_task", source: null, suggested_content: null });
  mount(); await openEditor(); await userEvent.click(screen.getByRole("button", { name: "Consult latest task report" }));
  expect(await screen.findByText("No eligible task report is available.")).toBeVisible();
  expect(screen.queryByRole("button", { name: "Use suggestion" })).not.toBeInTheDocument();
});

it("prevents using a report based on a newer checkpoint", async () => {
  mocks.api.getProjectContinuityDraft.mockResolvedValue({ ...draft, current: { ...current, revision: id } });
  mount(); await openEditor(); await userEvent.click(screen.getByRole("button", { name: "Consult latest task report" }));
  expect(await screen.findByRole("button", { name: "Use suggestion" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Publish memory" })).toBeDisabled();
  expect(mocks.api.publishProjectContinuity).not.toHaveBeenCalled();
});

it("renders load failures explicitly and can recover to an untouched empty checkpoint", async () => {
  mocks.api.getProjectContinuity.mockRejectedValueOnce(new Error("unavailable"));
  mount();
  expect(await screen.findByRole("alert")).toHaveTextContent("Could not load project memory");
  expect(screen.queryByRole("button", { name: "Review memory" })).not.toBeInTheDocument();
  mocks.api.getProjectContinuity.mockResolvedValue({ ...current, revision: "", content: "", reviewed_by: "", reviewed_at: "" });
  await userEvent.click(screen.getByRole("button", { name: "Try again" }));
  expect(await screen.findByText("No memory published yet.")).toBeVisible();
});

it("keeps publication pending until confirmed and prevents dismissal during the write", async () => {
  const pending = Promise.withResolvers<typeof current>();
  mocks.api.publishProjectContinuity.mockReturnValue(pending.promise);
  mount(); await userEvent.type(await openEditor(), " updated");
  await userEvent.click(screen.getByRole("button", { name: "Publish memory" }));
  expect(screen.getByRole("button", { name: "Publish memory" })).toBeDisabled();
  await userEvent.keyboard("{Escape}");
  expect(screen.getByRole("dialog")).toBeVisible();
  await act(async () => pending.resolve({ ...current, revision: id, content: "Published decision updated" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
});

it("resets the editor and scopes the query when the workspace changes", async () => {
  const { rerender, qc } = mount();
  await userEvent.type(await openEditor(), " workspace-one-only");
  mocks.wsId = "workspace-2";
  mocks.api.getProjectContinuity.mockResolvedValue({ ...current, content: "Workspace two" });
  rerender(<QueryClientProvider client={qc}><ProjectContinuitySection projectId={id} /></QueryClientProvider>);
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(await screen.findByText("Workspace two")).toBeVisible();
  expect(await openEditor()).toHaveValue("Workspace two");
  expect(qc.getQueryData(continuityKey("workspace-1", id))).toMatchObject({ content: "Published decision" });
});
