import { beforeEach, describe, expect, it, vi } from "vitest";

const postMock = vi.hoisted(() => vi.fn());

vi.mock("./api-client", () => ({
	apiClient: { POST: postMock },
	apiErrorMessage: vi.fn(() => "request failed"),
}));

import { spawnWorker } from "./spawn-worker";

describe("spawnWorker", () => {
	beforeEach(() => vi.resetAllMocks());

	it("creates a worker through the sessions API", async () => {
		postMock.mockResolvedValue({ data: { session: { id: "worker-1" } }, response: { status: 201 } });

		await expect(spawnWorker("project-1", "implement this")).resolves.toBe("worker-1");
		expect(postMock).toHaveBeenCalledWith("/api/v1/sessions", {
			body: { projectId: "project-1", kind: "worker", prompt: "implement this" },
		});
	});
});
