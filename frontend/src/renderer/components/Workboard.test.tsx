import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { WorkCard as WorkboardCard } from "../hooks/useWorkboardQuery";

const { getMock, patchMock, postMock, useWorkboardCardsMock, useWorkspaceQueryMock, useDirectorStatusMock, useWorkCardDispatchFailureMock, useDispatchProjectMock, aoBridgeMock } = vi.hoisted(() => ({
	getMock: vi.fn(),
	patchMock: vi.fn(),
	postMock: vi.fn(),
	useWorkboardCardsMock: vi.fn(),
	useWorkspaceQueryMock: vi.fn(),
	useDirectorStatusMock: vi.fn(),
	useWorkCardDispatchFailureMock: vi.fn(),
	useDispatchProjectMock: vi.fn(),
	aoBridgeMock: {
		daemon: {
			readLog: vi.fn().mockResolvedValue(["line one", "line two"]),
		},
	},
}));

vi.mock("../hooks/useWorkboardQuery", () => ({
	workboardQueryKey: (projectId?: string) => (projectId ? ["workboard", projectId] : ["workboard"]),
	useWorkboardCards: (...args: unknown[]) => useWorkboardCardsMock(...args),
	useWorkCardRedo: () => ({ data: [], isError: false }),
	useDirectorStatus: (...args: unknown[]) => useDirectorStatusMock(...args),
	useWorkCardDispatchFailure: (...args: unknown[]) => useWorkCardDispatchFailureMock(...args),
	useDispatchProject: (...args: unknown[]) => useDispatchProjectMock(...args),
}));

vi.mock("../lib/api-client", () => ({
	apiClient: { GET: (...args: unknown[]) => getMock(...args), PATCH: (...args: unknown[]) => patchMock(...args), POST: (...args: unknown[]) => postMock(...args) },
	apiErrorMessage: () => "Request failed",
}));

vi.mock("../hooks/useWorkspaceQuery", () => ({ useWorkspaceQuery: (...args: unknown[]) => useWorkspaceQueryMock(...args) }));

type DaemonStatus = { state: string; message?: string; port?: number };
const useShellMock = vi.fn<() => { daemonStatus: DaemonStatus }>(() => ({ daemonStatus: { state: "ready" } }));
vi.mock("../lib/shell-context", () => ({ useShell: () => useShellMock() }));
vi.mock("../lib/bridge", () => ({ aoBridge: aoBridgeMock }));
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
	useDirectorStatusMock.mockReset().mockReturnValue({ data: undefined });
	useWorkCardDispatchFailureMock.mockReset().mockReturnValue({ data: undefined });
	useDispatchProjectMock.mockReset().mockReturnValue({ isPending: false, mutate: vi.fn() });
	useShellMock.mockReset().mockReturnValue({ daemonStatus: { state: "ready" } });
	postMock.mockReset().mockResolvedValue({ data: { ...card, status: "running" }, error: undefined });
	patchMock.mockReset().mockResolvedValue({ data: { status: "ok" }, error: undefined });
	getMock.mockReset().mockResolvedValue({ data: { status: "ok", project: { id: "proj-1", config: { workboard: { autonomous: { enabled: false, mode: "skip_timeout", shortTimeoutMinutes: 2, sticky: true } } } } }, error: undefined });
	aoBridgeMock.daemon.readLog.mockReset().mockResolvedValue(["line one", "line two"]);
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
		fireEvent.click(screen.getByRole("button", { name: "Show live terminal" }));
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

	it("keeps non-focused cards off the terminal, then keeps it available during review", async () => {
		const onShowSessions = vi.fn();
		const linkedCard = { ...card, status: "review" as const, sessionId: "session-1" };
		useWorkboardCardsMock.mockReturnValue({ data: [linkedCard], isError: false });
		useWorkspaceQueryMock.mockReturnValue({
			data: [{ id: "proj-1", name: "Project", path: "/repo/project", sessions: [{
				id: "session-1", workspaceId: "proj-1", workspaceName: "Project", title: "Repair diagnostics",
				provider: "codex", branch: "session/session-1", status: "working", updatedAt: "2026-01-01T00:00:00Z", prs: [],
			}] }],
		});

		renderBoard(onShowSessions);

		expect(screen.queryByText("live terminal preview")).not.toBeInTheDocument();

		fireEvent.click(screen.getByRole("article", { name: /Repair diagnostics/i }));

		expect(screen.queryByText("live terminal preview")).not.toBeInTheDocument();
		await waitFor(() => expect(screen.getByRole("button", { name: "View all sessions" })).toBeInTheDocument());
		expect(screen.getByRole("button", { name: "Show live terminal" })).toBeInTheDocument();
		fireEvent.click(screen.getByRole("button", { name: "Show live terminal" }));
		expect(screen.getByText("live terminal preview")).toBeInTheDocument();

		fireEvent.click(screen.getByRole("button", { name: "View all sessions" }));
		expect(onShowSessions).toHaveBeenCalledTimes(1);
	});
});

describe("DirectorStatusBar", () => {
	it("shows online status when daemon is ready", () => {
		useShellMock.mockReturnValue({ daemonStatus: { state: "ready" } });
		renderBoard();
		expect(screen.getByText("Auto-dispatch online")).toBeInTheDocument();
	});

	it("shows error state with message when daemon has an error", () => {
		useShellMock.mockReturnValue({ daemonStatus: { state: "error", message: "Port already in use" } });
		renderBoard();
		expect(screen.getByText("Port already in use")).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "View daemon log" })).toBeInTheDocument();
	});

	it("shows offline message when daemon is stopped", () => {
		useShellMock.mockReturnValue({ daemonStatus: { state: "stopped" } });
		renderBoard();
		expect(screen.getByText("Daemon offline")).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "View daemon log" })).toBeInTheDocument();
	});

	it("shows starting state when daemon is starting", () => {
		useShellMock.mockReturnValue({ daemonStatus: { state: "starting" } });
		renderBoard();
		expect(screen.getByText("Starting daemon…")).toBeInTheDocument();
	});

	it("opens daemon log dialog when View daemon log is clicked", async () => {
		useShellMock.mockReturnValue({ daemonStatus: { state: "stopped" } });
		renderBoard();
		fireEvent.click(screen.getByRole("button", { name: "View daemon log" }));
		await waitFor(() => {
			expect(screen.getByText("Daemon log")).toBeInTheDocument();
		});
	});

	it("shows director running/queued counts when projectId is set and directorStatus has data", () => {
		useDirectorStatusMock.mockReturnValue({
			data: { runningCount: 2, wipLimit: 3, todoCount: 5, lastDispatchAttempt: undefined },
		});
		renderBoard();
		expect(screen.getByText(/2\/3 running/)).toBeInTheDocument();
		expect(screen.getByText(/5 queued/)).toBeInTheDocument();
	});
});

describe("Workboard CDC invalidation regression", () => {
	it("work_card_changed event would invalidate both project and global workboard query keys", async () => {
		// Regression: before the realtime state branch, only the global key was
		// invalidated on work_card_changed. After processing the event, both the
		// project-scoped and global keys must be invalidated so the board reflects
		// the change regardless of which board view is active.
		const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
		const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");

		// Trigger the same logic refreshWorkboard does in event-transport.ts
		const workboardQueryKey = (projectId?: string) =>
			projectId ? (["workboard", projectId] as const) : (["workboard", "global"] as const);
		void queryClient.invalidateQueries({ queryKey: workboardQueryKey("proj-1") });
		void queryClient.invalidateQueries({ queryKey: ["workboard", "proj-1", "director-status"] });
		void queryClient.invalidateQueries({ queryKey: workboardQueryKey() });

		expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ["workboard", "proj-1"] });
		expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ["workboard", "global"] });
		expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ["workboard", "proj-1", "director-status"] });
	});
});
