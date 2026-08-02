import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { WorkspaceSession, WorkspaceSummary } from "../types/workspace";

const { navigateMock, routeParams } = vi.hoisted(() => ({
	navigateMock: vi.fn(),
	routeParams: { sessionId: "orch-1" },
}));

vi.mock("@tanstack/react-router", async (importOriginal) => {
	const actual = await importOriginal<typeof import("@tanstack/react-router")>();
	return { ...actual, useNavigate: () => navigateMock, useParams: () => routeParams };
});

vi.mock("../hooks/useWorkspaceQuery", () => ({
	useWorkspaceQuery: (): { data: WorkspaceSummary[] } => ({
		data: [
			{
				id: "proj-1",
				name: "my-app",
				path: "/proj-1",
				sessions: [
					{
						id: "orch-1",
						workspaceId: "proj-1",
						workspaceName: "my-app",
						title: "orchestrator",
						provider: "claude-code",
						kind: "orchestrator",
						branch: "main",
						status: "working",
						updatedAt: "2026-06-10T00:00:00Z",
						prs: [],
					},
					{
						id: "worker-1",
						workspaceId: "proj-1",
						workspaceName: "my-app",
						title: "worker",
						provider: "claude-code",
						kind: "worker",
						branch: "ao/worker-1",
						status: "working",
						updatedAt: "2026-06-10T00:00:00Z",
						prs: [],
					},
				],
			},
		],
	}),
	workspaceQueryKey: ["workspaces"],
}));

// NotificationCenter pulls in SSE/IPC machinery irrelevant to the New
// task/Kanban removal under test (mirrors SessionView.test.tsx's treatment
// of unrelated terminal/inspector machinery).
vi.mock("./NotificationCenter", () => ({ NotificationCenter: () => <div /> }));

import { ShellTopbar, TopbarDeleteButton, TopbarKillButton } from "./ShellTopbar";

const { deleteMock, postMock } = vi.hoisted(() => ({
	deleteMock: vi.fn(),
	postMock: vi.fn(),
}));

vi.mock("../lib/api-client", () => ({
	apiClient: {
		DELETE: deleteMock,
		POST: postMock,
	},
	apiErrorMessage: (error: unknown, fallback = "Request failed") => {
		if (error instanceof Error) return error.message;
		if (typeof error === "object" && error !== null && "message" in error) {
			return String((error as { message: unknown }).message);
		}
		return fallback;
	},
}));

const worker: WorkspaceSession = {
	id: "sess-1",
	workspaceId: "proj-1",
	workspaceName: "my-app",
	title: "do the thing",
	provider: "claude-code",
	kind: "worker",
	branch: "ao/sess-1",
	status: "working",
	updatedAt: "2026-06-10T00:00:00Z",
	prs: [],
};

function renderKill(session: WorkspaceSession = worker) {
	const queryClient = new QueryClient({
		defaultOptions: {
			queries: { retry: false },
			mutations: { retry: false },
		},
	});
	render(
		<QueryClientProvider client={queryClient}>
			<TopbarKillButton session={session} />
		</QueryClientProvider>,
	);
	return queryClient;
}

beforeEach(() => {
	routeParams.sessionId = "orch-1";
	deleteMock.mockReset();
	deleteMock.mockResolvedValue({ data: { ok: true, sessionId: "sess-1" }, error: undefined });
	postMock.mockReset();
	postMock.mockResolvedValue({ data: { ok: true, sessionId: "sess-1" }, error: undefined });
});

describe("TopbarDeleteButton", () => {
	it("requires confirmation before permanently deleting a session", async () => {
		const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
		render(<QueryClientProvider client={queryClient}><TopbarDeleteButton session={worker} /></QueryClientProvider>);

		await userEvent.click(screen.getByRole("button", { name: "Delete session" }));
		expect(deleteMock).not.toHaveBeenCalled();
		await userEvent.click(screen.getByRole("button", { name: "Confirm delete" }));

		await waitFor(() => expect(deleteMock).toHaveBeenCalledWith("/api/v1/sessions/{sessionId}", {
			params: { path: { sessionId: "sess-1" } },
		}));
	});
});

describe("TopbarKillButton", () => {
	it("arms a confirmation before killing an active session", async () => {
		renderKill();

		await userEvent.click(screen.getByRole("button", { name: "Kill session" }));
		expect(postMock).not.toHaveBeenCalled();

		await userEvent.click(screen.getByRole("button", { name: "Confirm kill" }));

		await waitFor(() => expect(postMock).toHaveBeenCalledTimes(1));
		expect(postMock).toHaveBeenCalledWith("/api/v1/sessions/{sessionId}/kill", {
			params: { path: { sessionId: "sess-1" } },
		});
	});

	it("can back out of the confirmation without killing", async () => {
		renderKill();

		await userEvent.click(screen.getByRole("button", { name: "Kill session" }));
		await userEvent.click(screen.getByRole("button", { name: "Cancel" }));

		expect(screen.getByRole("button", { name: "Kill session" })).toBeInTheDocument();
		expect(postMock).not.toHaveBeenCalled();
	});

	it("surfaces the daemon error when the kill fails", async () => {
		postMock.mockResolvedValue({ data: undefined, error: { message: "session not found" } });
		renderKill();

		await userEvent.click(screen.getByRole("button", { name: "Kill session" }));
		await userEvent.click(screen.getByRole("button", { name: "Confirm kill" }));

		expect(await screen.findByText("session not found")).toBeInTheDocument();
	});
});

describe("ShellTopbar on an orchestrator session (Code mode)", () => {
	it("does not offer New task or Kanban — that surface moved to Director mode", () => {
		const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
		render(
			<QueryClientProvider client={queryClient}>
				<ShellTopbar />
			</QueryClientProvider>,
		);

		expect(screen.queryByRole("button", { name: "New task" })).not.toBeInTheDocument();
		expect(screen.queryByRole("button", { name: "Open Kanban" })).not.toBeInTheDocument();
	});
});

describe("ShellTopbar on a worker session (Code mode)", () => {
	it("keeps the orchestrator launcher out of the Code session header", () => {
		routeParams.sessionId = "worker-1";
		const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
		render(
			<QueryClientProvider client={queryClient}>
				<ShellTopbar />
			</QueryClientProvider>,
		);

		expect(screen.queryByRole("button", { name: "Open orchestrator" })).not.toBeInTheDocument();
	});
});
