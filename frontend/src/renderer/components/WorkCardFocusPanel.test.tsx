import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { WorkCard } from "../hooks/useWorkboardQuery";

const { patchMock } = vi.hoisted(() => ({
	patchMock: vi.fn(),
}));

vi.mock("../lib/api-client", () => ({
	apiClient: {
		PATCH: (...args: unknown[]) => patchMock(...args),
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
	waitingForInput: false,
	pausedRetarget: false,
	goalVersion: 1,
	createdAt: "2026-07-17T08:00:00.000Z",
	updatedAt: "2026-07-17T08:00:00.000Z",
};

function renderPanel(card: WorkCard = scheduledCard) {
	render(
		<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
			<WorkCardFocusPanel card={card} projectId="proj-1" theme="dark" daemonReady onClose={vi.fn()} />
		</QueryClientProvider>,
	);
}

beforeEach(() => {
	patchMock.mockReset().mockResolvedValue({ error: undefined });
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
