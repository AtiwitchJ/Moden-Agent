import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
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
			supported: [{ id: "hermes", label: "Hermes" }, { id: "codex", label: "Codex" }, { id: "claude-code", label: "Claude Code" }],
			installed: [{ id: "hermes", label: "Hermes", authStatus: "authorized" }, { id: "codex", label: "Codex", authStatus: "authorized" }, { id: "claude-code", label: "Claude Code", authStatus: "authorized" }],
			authorized: [{ id: "hermes", label: "Hermes", authStatus: "authorized" }, { id: "codex", label: "Codex", authStatus: "authorized" }, { id: "claude-code", label: "Claude Code", authStatus: "authorized" }],
		},
		error: undefined,
	});
	postMock.mockReset();
});

describe("CreateWorkCardDialog", () => {
	it("registers a selected folder with Director when no project exists", async () => {
		render(
			<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
				<CreateWorkCardDialog open onCreated={vi.fn()} onOpenChange={vi.fn()} />
			</QueryClientProvider>,
		);
		const user = userEvent.setup();
		postMock
			.mockResolvedValueOnce({ data: { project: { id: "folder-project", name: "folder-project", path: "/repo/project" } }, error: undefined })
			.mockResolvedValueOnce({ data: { id: "card-1" }, error: undefined });

		await user.type(screen.getByLabelText("Title *"), "Repair build diagnostics");
		await user.type(screen.getByLabelText("Folder *"), "/repo/project");
		await user.click(screen.getByRole("button", { name: "Create card" }));

		expect(screen.queryByText("Select project")).not.toBeInTheDocument();
		expect(postMock).toHaveBeenNthCalledWith(1, "/api/v1/projects", {
			body: {
				path: "/repo/project",
				config: {
					worker: { agent: "codex" },
					orchestrator: { agent: "hermes" },
					director: { agent: "director" },
					workboard: { wipLimit: 4 },
				},
			},
		});
		expect(postMock).toHaveBeenNthCalledWith(2, "/api/v1/workboard/cards", {
			body: expect.objectContaining({ projectId: "folder-project", targetPath: "/repo/project", codingAgent: "codex" }),
		});
	});

	it("assigns Director ownership while allowing each worker phase to be selected", async () => {
		renderDialog();

		await waitFor(() => expect(screen.getByRole("combobox", { name: "Coding agent" })).toHaveTextContent("Codex"));
		expect(screen.getByRole("combobox", { name: "Review agent" })).toHaveTextContent("Codex");
		expect(screen.getByRole("combobox", { name: "Testing agent" })).toHaveTextContent("Codex");
		expect(screen.getByText(/Director owns the card/i)).toBeInTheDocument();
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
		await user.type(screen.getByLabelText("Folder *"), "/repo/project");
		await user.type(screen.getByLabelText("Labels"), "frontend{Enter}");
		await user.click(screen.getByRole("combobox", { name: "Review agent" }));
		await user.click(screen.getByRole("option", { name: "Claude Code" }));
		await user.click(screen.getByRole("combobox", { name: "Testing agent" }));
		await user.click(screen.getByRole("option", { name: "Codex" }));
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
				reviewerMode: "separate",
				reviewerAgent: "claude-code",
				testingAgent: "codex",
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
		await user.type(screen.getByLabelText("Folder *"), "/repo/project");
		await user.type(screen.getByLabelText("Labels"), "frontend{Enter}");
		await user.click(screen.getByRole("button", { name: "Create card" }));

		const spinner = document.querySelector(".animate-spin");
		expect(spinner).toHaveClass("motion-reduce:animate-none");
	});
});
