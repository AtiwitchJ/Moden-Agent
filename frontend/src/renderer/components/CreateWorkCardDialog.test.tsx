import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { getMock, postMock } = vi.hoisted(() => ({
	getMock: vi.fn(),
	postMock: vi.fn(),
}));

vi.mock("../lib/workboard-schedule", async () => {
	const actual = await vi.importActual<typeof import("../lib/workboard-schedule")>("../lib/workboard-schedule");
	return {
		...actual,
		defaultScheduleValue: () => "2026-07-17T15:30",
		parseDatetimeLocalValue: (value: string) => (value.trim() ? "2026-07-17T15:30:00.000Z" : undefined),
	};
});

vi.mock("../lib/api-client", () => ({
	apiClient: {
		GET: (...args: unknown[]) => getMock(...args),
		POST: (...args: unknown[]) => postMock(...args),
	},
	apiErrorMessage: (error: unknown, fallback = "Request failed") =>
		typeof error === "object" && error !== null && "message" in error ? String(error.message) : fallback,
}));

import { CreateWorkCardDialog } from "./CreateWorkCardDialog";

function renderDialog() {
	render(
		<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
			<CreateWorkCardDialog open projectId="proj-1" onCreated={vi.fn()} onOpenChange={vi.fn()} />
		</QueryClientProvider>,
	);
}

beforeEach(() => {
	getMock.mockReset().mockResolvedValue({
		data: {
			supported: [{ id: "codex", label: "Codex" }],
			installed: [{ id: "codex", label: "Codex", authStatus: "authorized" }],
			authorized: [{ id: "codex", label: "Codex", authStatus: "authorized" }],
		},
		error: undefined,
	});
	postMock.mockReset();
});

describe("CreateWorkCardDialog", () => {
	it("refuses submit without project in global context", async () => {
		render(
			<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
				<CreateWorkCardDialog open onCreated={vi.fn()} onOpenChange={vi.fn()} />
			</QueryClientProvider>,
		);
		const user = userEvent.setup();

		await user.type(screen.getByLabelText("Title *"), "Repair build diagnostics");
		await user.click(screen.getByRole("button", { name: "Create card" }));

		expect(await screen.findByText("Select a project before creating this card.")).toBeInTheDocument();
		expect(postMock).not.toHaveBeenCalled();
	});

	it("refuses submit until an agent is selected", async () => {
		renderDialog();
		const user = userEvent.setup();

		await user.type(screen.getByLabelText("Title *"), "Repair build diagnostics");
		await user.type(screen.getByLabelText("Notes"), "Keep compiler errors actionable.");
		await user.type(screen.getByLabelText("Folder"), "/repo/project");
		await user.type(screen.getByLabelText("Labels"), "frontend{Enter}");
		await user.click(screen.getByRole("button", { name: "Create card" }));

		expect(await screen.findByText("Select an agent before creating this card.")).toBeInTheDocument();
		expect(postMock).not.toHaveBeenCalled();
	});

	it("creates a scheduled card when schedule for later is enabled", async () => {
		postMock.mockResolvedValue({
			data: {
				id: "card_1",
				projectId: "proj-1",
				boardId: "default",
				title: "Repair build diagnostics",
				notes: "Keep compiler errors actionable.",
				priority: "normal",
				labels: ["frontend"],
				status: "scheduled",
				scheduledAt: "2026-07-17T08:30:00.000Z",
				position: 0,
				targetPath: "/repo/project",
				agent: "codex",
				waitingForInput: false,
				pausedRetarget: false,
				goalVersion: 1,
				createdAt: "2026-07-17T08:00:00.000Z",
				updatedAt: "2026-07-17T08:00:00.000Z",
			},
			error: undefined,
		});
		renderDialog();
		const user = userEvent.setup();

		await user.type(screen.getByLabelText("Title *"), "Repair build diagnostics");
		await user.type(screen.getByLabelText("Notes"), "Keep compiler errors actionable.");
		await user.type(screen.getByLabelText("Folder"), "/repo/project");
		await user.type(screen.getByLabelText("Labels"), "frontend{Enter}");
		await user.click(screen.getByLabelText("Coding Agent *"));
		await user.click(await screen.findByRole("option", { name: "Codex" }));
		await user.click(screen.getByRole("checkbox", { name: /Schedule for later/i }));
		await user.click(screen.getByRole("button", { name: "Create card" }));

		expect(postMock).toHaveBeenCalledWith("/api/v1/projects/{projectId}/workboard/cards", {
			params: { path: { projectId: "proj-1" } },
			body: {
				projectId: "proj-1",
				title: "Repair build diagnostics",
				notes: "Keep compiler errors actionable.",
				targetPath: "/repo/project",
				labels: ["frontend"],
				priority: "normal",
				agent: "codex",
				codingAgent: "codex",
				reviewerMode: "same",
				status: "scheduled",
				scheduledAt: "2026-07-17T15:30:00.000Z",
			},
		});
	});

	it("disables the creating spinner for reduced-motion users", async () => {
		postMock.mockReturnValue(new Promise(() => undefined));
		renderDialog();
		const user = userEvent.setup();

		await user.type(screen.getByLabelText("Title *"), "Repair build diagnostics");
		await user.type(screen.getByLabelText("Notes"), "Keep compiler errors actionable.");
		await user.type(screen.getByLabelText("Folder"), "/repo/project");
		await user.type(screen.getByLabelText("Labels"), "frontend{Enter}");
		await user.click(screen.getByLabelText("Coding Agent *"));
		await user.click(await screen.findByRole("option", { name: "Codex" }));
		await user.click(screen.getByRole("button", { name: "Create card" }));

		const spinner = document.querySelector(".animate-spin");
		expect(spinner).toHaveClass("motion-reduce:animate-none");
	});
});
