import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

const { navigateMock, spawnWorkerMock, createProjectMock } = vi.hoisted(() => ({
	navigateMock: vi.fn(),
	spawnWorkerMock: vi.fn(),
	createProjectMock: vi.fn(),
}));

vi.mock("@tanstack/react-router", async (importOriginal) => {
	const actual = await importOriginal<typeof import("@tanstack/react-router")>();
	return { ...actual, useNavigate: () => navigateMock };
});

vi.mock("../lib/spawn-worker", () => ({ spawnWorker: spawnWorkerMock }));
vi.mock("../lib/shell-context", () => ({ useShell: () => ({ createProject: createProjectMock }) }));
vi.mock("../hooks/useWorkspaceQuery", () => ({
	useWorkspaceQuery: () => ({
		data: [{ id: "proj1", name: "Proj One", path: "/proj1", sessions: [] }],
	}),
}));

import { CodeHomeComposer } from "./CodeHomeComposer";

function renderComposer() {
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={queryClient}>
			<CodeHomeComposer />
		</QueryClientProvider>,
	);
}

describe("CodeHomeComposer", () => {
	it("focuses the prompt input on mount", () => {
		renderComposer();
		expect(screen.getByLabelText("Prompt")).toHaveFocus();
	});

	it("existing project: starts a coding worker with the prompt and navigates", async () => {
		const user = userEvent.setup();
		spawnWorkerMock.mockResolvedValue("sess1");
		renderComposer();

		await user.click(screen.getByLabelText("Project"));
		await user.click(await screen.findByRole("option", { name: "Proj One" }));
		await user.type(screen.getByLabelText("Prompt"), "hello there");
		await user.click(screen.getByRole("button", { name: "Send" }));

		await waitFor(() => expect(spawnWorkerMock).toHaveBeenCalledWith("proj1", "hello there"));
		await waitFor(() =>
			expect(navigateMock).toHaveBeenCalledWith({
				to: "/projects/$projectId/sessions/$sessionId",
				params: { projectId: "proj1", sessionId: "sess1" },
			}),
		);
	});
});
