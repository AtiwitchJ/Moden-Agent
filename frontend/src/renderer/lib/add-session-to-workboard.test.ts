import { describe, expect, it, vi, beforeEach } from "vitest";
import type { WorkspaceSession, WorkspaceSummary } from "../types/workspace";

const { postMock, patchMock } = vi.hoisted(() => ({
	postMock: vi.fn(),
	patchMock: vi.fn(),
}));

vi.mock("./api-client", () => ({
	apiClient: { POST: postMock, PATCH: patchMock },
	apiErrorMessage: (_error: unknown, fallback = "Request failed") => fallback,
}));

import { addSessionToWorkboard } from "./add-session-to-workboard";

const workspace: WorkspaceSummary = {
	id: "proj-1",
	name: "proj-1",
	path: "/repo/proj-1",
	sessions: [],
};

const session: WorkspaceSession = {
	id: "sess-1",
	workspaceId: "proj-1",
	workspaceName: "proj-1",
	title: "Fix checkout",
	provider: "codex",
	kind: "worker",
	branch: "feat/checkout",
	status: "working",
	updatedAt: "2026-01-01T00:00:00Z",
	prs: [],
};

describe("addSessionToWorkboard", () => {
	beforeEach(() => {
		postMock.mockReset();
		patchMock.mockReset();
	});

	it("creates a running card with the session link in one request", async () => {
		postMock.mockResolvedValueOnce({
			data: {
				id: "card-1",
				projectId: "proj-1",
				status: "running",
				title: "Fix checkout",
				sessionId: "sess-1",
			},
			error: undefined,
		});

		const card = await addSessionToWorkboard(session, workspace);

		expect(postMock).toHaveBeenCalledWith("/api/v1/projects/{projectId}/workboard/cards", {
			params: { path: { projectId: "proj-1" } },
			body: expect.objectContaining({
				title: "Fix checkout",
				status: "running",
				targetPath: "/repo/proj-1",
				agent: "codex",
				sessionId: "sess-1",
			}),
		});
		expect(patchMock).not.toHaveBeenCalled();
		expect(card.sessionId).toBe("sess-1");
	});
});
