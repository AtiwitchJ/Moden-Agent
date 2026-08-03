import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { WorkCard } from "../hooks/useWorkboardQuery";
import type { WorkspaceSession } from "../types/workspace";

const { deleteMock, patchMock, getMock } = vi.hoisted(() => ({
	deleteMock: vi.fn(),
	patchMock: vi.fn(),
	getMock: vi.fn(),
}));

vi.mock("../lib/api-client", () => ({
	apiClient: {
		DELETE: (...args: unknown[]) => deleteMock(...args),
		PATCH: (...args: unknown[]) => patchMock(...args),
		GET: (...args: unknown[]) => getMock(...args),
		POST: vi.fn(),
	},
	apiErrorMessage: (error: unknown, fallback = "Request failed") =>
		typeof error === "object" && error !== null && "message" in error ? String(error.message) : fallback,
}));

vi.mock("../lib/workboard-schedule", async () => {
	const actual = await vi.importActual<typeof import("../lib/workboard-schedule")>("../lib/workboard-schedule");
	return {
		...actual,
		formatDatetimeLocalValue: () => "2026-07-17T15:30",
		parseDatetimeLocalValue: (value: string) => (value.trim() ? "2026-07-17T16:00:00.000Z" : undefined),
	};
});

vi.mock("./TerminalPane", () => ({ TerminalPane: () => <div>live terminal preview</div> }));

import { WorkCardFocusPanel } from "./WorkCardFocusPanel";

const scheduledCard: WorkCard = {
	id: "card_1",
	projectId: "proj-1",
	boardId: "default",
	title: "Scheduled card",
	notes: "Notes",
	priority: "normal",
	labels: ["api"],
	status: "scheduled",
	scheduledAt: "2026-07-17T15:30:00.000Z",
	position: 0,
	targetPath: "/repo/project",
	agent: "codex",
	redoCount: 0,
	waitingForInput: false,
	pausedRetarget: false,
	goalVersion: 1,
	createdAt: "2026-07-17T08:00:00.000Z",
	updatedAt: "2026-07-17T08:00:00.000Z",
};

function renderPanel(card: WorkCard = scheduledCard, session?: WorkspaceSession) {
	render(
		<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
			<WorkCardFocusPanel card={card} projectId="proj-1" session={session} theme="dark" daemonReady onClose={vi.fn()} />
		</QueryClientProvider>,
	);
}

beforeEach(() => {
	deleteMock.mockReset().mockResolvedValue({ error: undefined });
	patchMock.mockReset().mockResolvedValue({ error: undefined });
	getMock.mockReset().mockResolvedValue({
		data: { projectId: "proj-1", daemonReady: true, runningCount: 1, todoCount: 2, wipLimit: 4 },
		error: undefined,
	});
});

describe("WorkCardFocusPanel schedule edit", () => {
	it("patches scheduledAt for scheduled cards", async () => {
		renderPanel();
		const user = userEvent.setup();

		await user.click(screen.getByRole("button", { name: "Save schedule" }));

		expect(patchMock).toHaveBeenCalledWith("/api/v1/workboard/cards/{cardId}", {
			params: { path: { cardId: "card_1" } },
			body: { scheduledAt: "2026-07-17T16:00:00.000Z" },
		});
	});
});

it("identifies a linked Hermes orchestrator as the commander", async () => {
	const card: WorkCard = { ...scheduledCard, status: "running", sessionId: "hermes-1" };
	const session: WorkspaceSession = {
		id: "hermes-1", workspaceId: "proj-1", workspaceName: "Project", title: "Hermes",
		provider: "codex", harness: "hermes", kind: "orchestrator", branch: "main", status: "working", updatedAt: "2026-07-17T08:00:00.000Z", prs: [],
	};
	render(
		<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
			<WorkCardFocusPanel card={card} projectId="proj-1" session={session} theme="dark" daemonReady onClose={vi.fn()} />
		</QueryClientProvider>,
	);
	expect(screen.getByText("Commander")).toBeInTheDocument();
	expect(screen.getByText("hermes")).toBeInTheDocument();
	expect(screen.getByText(/stays responsible through review and testing/i)).toBeInTheDocument();
	expect(screen.queryByRole("button", { name: /commander terminal/i })).not.toBeInTheDocument();
	await userEvent.setup().click(screen.getByRole("button", { name: "Card actions" }));
	expect(screen.getByText("Nudge commander")).toBeInTheDocument();
});

it("requires confirmation before deleting a card", async () => {
	const card: WorkCard = { ...scheduledCard, status: "running", sessionId: "hermes-1" };
	const session: WorkspaceSession = {
		id: "hermes-1", workspaceId: "proj-1", workspaceName: "Project", title: "Hermes",
		provider: "codex", harness: "hermes", kind: "orchestrator", branch: "main", status: "working", updatedAt: "2026-07-17T08:00:00.000Z", prs: [],
	};
	render(
		<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
			<WorkCardFocusPanel card={card} projectId="proj-1" session={session} theme="dark" daemonReady onClose={vi.fn()} />
		</QueryClientProvider>,
	);
	const user = userEvent.setup();
	await user.click(screen.getByRole("button", { name: "Card actions" }));
	await user.click(screen.getByText("Delete card"));
	expect(screen.getByText("Delete this card permanently?")).toBeInTheDocument();
	expect(deleteMock).not.toHaveBeenCalled();

	await user.click(screen.getByRole("button", { name: "Confirm delete card" }));
	await waitFor(() => expect(deleteMock).toHaveBeenCalledWith("/api/v1/workboard/cards/{cardId}", {
		params: { path: { cardId: "card_1" } },
	}));
});

it("renders commander UI for a Director session, not just Hermes", () => {
	const card: WorkCard = { ...scheduledCard, status: "running", sessionId: "director-1" };
	const session: WorkspaceSession = {
		id: "director-1", workspaceId: "proj-1", workspaceName: "Project", title: "Director",
		provider: "codex", harness: "director", kind: "orchestrator", branch: "main", status: "working", updatedAt: "2026-07-17T08:00:00.000Z", prs: [],
	};
	render(
		<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
			<WorkCardFocusPanel card={card} projectId="proj-1" session={session} theme="dark" daemonReady onClose={vi.fn()} />
		</QueryClientProvider>,
	);
	expect(screen.getByText("Commander")).toBeInTheDocument();
	expect(screen.queryByText("Linked session")).not.toBeInTheDocument();
});

it("shows the card brief for a commander session without a terminal button", async () => {
	const card: WorkCard = { ...scheduledCard, status: "running", sessionId: "hermes-1", notes: "Update the palette and verify the contrast." };
	const session: WorkspaceSession = {
		id: "hermes-1", workspaceId: "proj-1", workspaceName: "Project", title: "Hermes",
		provider: "codex", harness: "hermes", kind: "orchestrator", branch: "main", status: "working", updatedAt: "2026-07-17T08:00:00.000Z", prs: [],
	};
	render(
		<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
			<WorkCardFocusPanel card={card} projectId="proj-1" session={session} theme="dark" daemonReady onClose={vi.fn()} />
		</QueryClientProvider>,
	);

	expect(screen.getByText("Task details")).toBeInTheDocument();
	expect(screen.getByText("Update the palette and verify the contrast.")).toBeInTheDocument();
	expect(screen.getByText("hermes")).toBeInTheDocument();
	expect(screen.queryByText("live terminal preview")).not.toBeInTheDocument();
	expect(screen.queryByRole("button", { name: /commander terminal/i })).not.toBeInTheDocument();
	expect(screen.queryByRole("button", { name: /live terminal/i })).not.toBeInTheDocument();
});

describe("WorkCardFocusPanel commander display", () => {
	const runningCard: WorkCard = { ...scheduledCard, id: "card_2", status: "running", scheduledAt: undefined, sessionId: "test-13" };

	it("shows a Director status summary instead of a commander terminal", async () => {
		renderPanel(runningCard, { id: "test-13", kind: "orchestrator", harness: "director" } as WorkspaceSession);

		expect(await screen.findByText("Commander")).toBeInTheDocument();
		expect(screen.getByText("director")).toBeInTheDocument();
		expect(screen.queryByRole("button", { name: /commander terminal/i })).not.toBeInTheDocument();
		expect(screen.queryByRole("button", { name: /live terminal/i })).not.toBeInTheDocument();
	});

	it("reports the project's board counts for a commander", async () => {
		renderPanel(runningCard, { id: "test-13", kind: "orchestrator", harness: "director" } as WorkspaceSession);

		await waitFor(() => expect(screen.getByText(/1 running · 2 to do · WIP 4/)).toBeInTheDocument());
	});

	it("still offers a live terminal for a worker session", async () => {
		const workerCard: WorkCard = { ...runningCard, sessionId: "test-11" };
		renderPanel(workerCard, { id: "test-11", kind: "worker", harness: "codex" } as WorkspaceSession);

		expect(await screen.findByRole("button", { name: /show live terminal/i })).toBeInTheDocument();
	});
});
