import type { ReactNode } from "react";
import { beforeEach, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import { HierarchyEscalations } from "./hierarchy-escalations";

const refresh = vi.fn();
const next = vi.fn();
let state: Record<string, unknown>;
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace" }));
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ issueDetail: (id: string) => `/workspace/issues/${id}` }) }));
vi.mock("../../navigation", () => ({ AppLink: ({ href, children }: { href: string; children: ReactNode }) => <a href={href}>{children}</a> }));
vi.mock("@multica/core/issues", () => ({ hierarchyEscalationsOptions: () => ({ queryKey: ["escalations"] }) }));
vi.mock("@tanstack/react-query", () => ({ useInfiniteQuery: () => state }));
beforeEach(() => { refresh.mockReset(); next.mockReset(); state = { isSuccess:true, refetch:refresh, fetchNextPage:next, hasNextPage:true, data:{pages:[{items:[{ request_id:"request", source_issue_id:"source",source_agent_id:"worker",source_agent_name:"Worker", manager_agent_id:null,human_intervention:true,decision_issue_id:"decision",decision_status:"in_review",reason:"Need approval",context:"Partial work retained",repair:"dispatch_not_recorded" }]}]} }; });
function open(){const result=renderWithI18n(<HierarchyEscalations issueId="source" />);const details=result.container.querySelector("details")!;details.open=true;fireEvent(details,new Event("toggle"));return result;}
it("shows the human decision, preserved context, links, diagnostic and bounded navigation", () => {
 open();expect(screen.getByText("Human intervention")).toBeInTheDocument();expect(screen.getByText("Need approval")).toBeInTheDocument();expect(screen.getByText("Partial work retained")).toBeInTheDocument();expect(screen.getByRole("link",{name:"Open decision"})).toHaveAttribute("href","/workspace/issues/decision");expect(screen.getByText(/Dispatch not recorded/)).toBeInTheDocument();fireEvent.click(screen.getByRole("button",{name:"Refresh"}));expect(refresh).toHaveBeenCalledOnce();fireEvent.click(screen.getByRole("button",{name:"Load older requests"}));expect(next).toHaveBeenCalledOnce();
});
it("keeps failed reads distinct from an empty history", () => {
 state={isError:true,refetch:refresh};open();expect(screen.getByRole("alert")).toHaveTextContent("Could not load escalations");expect(screen.queryByText("No escalation requests.")).not.toBeInTheDocument();
});
