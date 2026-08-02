import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { WorkCard as WorkboardCard } from "../hooks/useWorkboardQuery";

const { getMock, patchMock, postMock, useWorkboardCardsMock, useWorkspaceQueryMock } = vi.hoisted(() => ({
	getMock: vi.fn(),
	patchMock: vi.fn(),
	postMock: vi.fn(),
	useWorkboardCardsMock: vi.fn(),
	useWorkspaceQueryMock: vi.fn(),
}));

vi.mock("../hooks/useWorkboardQuery", () => ({
	workboardQueryKey: (projectId?: string) => (projectId ? ["workboard", projectId] : ["workboard"]),
	useWorkboardCards: (...args: unknown[]) => useWorkboardCardsMock(...args),
	useWorkCardRedo: () => ({ data: [], isError: false }),
}));

vi.mock("../lib/api-client", () => ({
	apiClient: { GET: (...args: unknown[]) => getMock(...args), PATCH: (...args: unknown[]) => patchMock(...args), POST: (...args: unknown[]) => postMock(...args) },
	apiErrorMessage: () => "Request failed",
}));

vi.mock("../hooks/useWorkspaceQuery", () => ({ useWorkspaceQuery: (...args: unknown[]) => useWorkspaceQueryMock(...args) }));
vi.mock("../lib/shell-context", () => ({ useShell: () => ({ daemonStatus: { state: "ready" } }) }));
vi.mock("./TerminalPane", () => ({ TerminalPane: () => <div>live terminal preview</div> }));

import { Workboard } from "./Workboard";

const card: WorkboardCard = {
	id: "card-1",
	projectId: "proj-1",
	boardId: "default",
	title: "Repair diagnostics",
	notes: "Preserve actionable errors.",
	priority: "high",
	labels: ["frontend"],
	status: "todo",
	position: 0,
	targetPath: "/repo/project",
	agent: "codex",
	redoCount: 0,
	waitingForInput: false,
	pausedRetarget: false,
	goalVersion: 1,
	createdAt: "2026-01-01T00:00:00Z",
	updatedAt: "2026-01-01T00:00:00Z",
};

function renderBoard(onShowSessions?: () => void) {
	render(
		<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
			<Workboard onShowSessions={onShowSessions} projectId="proj-1" />
		</QueryClientProvider>,
	);
}

beforeEach(() => {
	useWorkboardCardsMock.mockReset().mockReturnValue({ data: [card], isError: false });
	useWorkspaceQueryMock.mockReset().mockReturnValue({ data: [] });
	postMock.mockReset().mockResolvedValue({ data: { ...card, status: "running" }, error: undefined });
	patchMock.mockReset().mockResolvedValue({ data: { status: "ok" }, error: undefined });
	getMock.mockReset().mockResolvedValue({ data: { status: "ok", project: { id: "proj-1", config: { workboard: { autonomous: { enabled: false, mode: "skip_timeout", shortTimeoutMinutes: 2, sticky: true } } } } }, error: undefined });
});

describe("Workboard", () => {
	it("renders columns in OpenClaw flow order", async () => {
		renderBoard();
		expect(screen.getAllByText(/^(Todo|Running|Review|Testing|Redo|Done)$/).map((node) => node.textContent)).toEqual([
			"Todo", "Running", "Review", "Testing", "Redo", "Done",
		]);
	});

	it("renders a card when an older daemon returns null labels", () => {
		useWorkboardCardsMock.mockReturnValue({ data: [{ ...card, labels: null as unknown as string[] }], isError: false });

		renderBoard();

		expect(screen.getByRole("article", { name: /Repair diagnostics/i })).toBeInTheDocument();
	});

	it("opens a live terminal preview for a selected running card", () => {
		const runningCard = { ...card, status: "running" as const, sessionId: "session-1" };
		useWorkboardCardsMock.mockReturnValue({ data: [runningCard], isError: false });
		useWorkspaceQueryMock.mockReturnValue({
			data: [{ id: "proj-1", name: "Project", path: "/repo/project", sessions: [{
				id: "session-1",
				workspaceId: "proj-1",
				workspaceName: "Project",
				title: "Repair diagnostics",
				provider: "codex",
				branch: "session/session-1",
				status: "working",
				updatedAt: "2026-01-01T00:00:00Z",
				prs: [],
			}] }],
		});

		renderBoard();
		fireEvent.click(screen.getByRole("article", { name: /Repair diagnostics/i }));

		expect(screen.getByRole("complementary", { name: /Focus panel for Repair diagnostics/i })).toBeInTheDocument();
		expect(screen.getByText("live terminal preview")).toBeInTheDocument();
	});

	it("moves a focused card with the arrow keys", async () => {
		renderBoard();
		fireEvent.keyDown(screen.getByRole("article", { name: /Repair diagnostics/i }), { key: "ArrowRight" });

		await waitFor(() => expect(postMock).toHaveBeenCalledWith("/api/v1/workboard/cards/{cardId}/move", {
			params: { path: { cardId: "card-1" } },
			body: { status: "running", position: 0 },
		}));
	});

	it("keeps non-focused cards off the live terminal and offers the fallback terminal action", async () => {
		const onShowSessions = vi.fn();
		const linkedCard = { ...card, status: "review" as const, sessionId: "session-1" };
		useWorkboardCardsMock.mockReturnValue({ data: [linkedCard], isError: false });

		renderBoard(onShowSessions);

		expect(screen.queryByText("live terminal preview")).not.toBeInTheDocument();

		fireEvent.click(screen.getByRole("article", { name: /Repair diagnostics/i }));

		expect(screen.queryByText("live terminal preview")).not.toBeInTheDocument();
		await waitFor(() => expect(screen.getByRole("button", { name: "Open terminal" })).toBeInTheDocument());

		fireEvent.click(screen.getByRole("button", { name: "Open terminal" }));
		expect(onShowSessions).toHaveBeenCalledTimes(1);
	});
});
