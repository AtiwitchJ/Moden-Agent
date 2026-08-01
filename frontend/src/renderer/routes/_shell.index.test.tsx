import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import type { WorkspaceSummary } from "../types/workspace";

const { navigateMock } = vi.hoisted(() => ({ navigateMock: vi.fn() }));

vi.mock("@tanstack/react-router", async (importOriginal) => {
	const actual = await importOriginal<typeof import("@tanstack/react-router")>();
	return { ...actual, useNavigate: () => navigateMock, createFileRoute: () => (opts: unknown) => opts };
});

vi.mock("../components/CodeHomeComposer", () => ({ CodeHomeComposer: () => <div data-testid="composer" /> }));
vi.mock("../stores/ui-store", () => ({
	useUiStore: (selector: (state: { orgName: string }) => unknown) => selector({ orgName: "Vertex Holdings" }),
}));

const { useWorkspaceQueryMock } = vi.hoisted(() => ({ useWorkspaceQueryMock: vi.fn() }));
vi.mock("../hooks/useWorkspaceQuery", () => ({ useWorkspaceQuery: useWorkspaceQueryMock }));

import { CodeHome } from "./_shell.index";

function renderHome(workspaces: WorkspaceSummary[]) {
	useWorkspaceQueryMock.mockReturnValue({ data: workspaces, isLoading: false });
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={queryClient}>
			<CodeHome />
		</QueryClientProvider>,
	);
}

describe("CodeHome", () => {
	it("greets by org name and always renders the composer", () => {
		renderHome([]);
		expect(screen.getByText("Welcome back, Vertex Holdings")).toBeInTheDocument();
		expect(screen.getByTestId("composer")).toBeInTheDocument();
	});

	it("shows an empty state when there are no sessions", () => {
		renderHome([]);
		expect(screen.getByText(/No sessions yet/)).toBeInTheDocument();
	});

	it("lists sessions needing attention before the rest, and navigates on click", async () => {
		const user = userEvent.setup();
		const workspaces: WorkspaceSummary[] = [
			{
				id: "proj1",
				name: "Proj One",
				path: "/proj1",
				sessions: [
					{
						id: "working-sess",
						workspaceId: "proj1",
						workspaceName: "Proj One",
						title: "Working session",
						provider: "claude-code",
						branch: "main",
						status: "working",
						updatedAt: "2026-08-02T00:00:00Z",
						prs: [],
					},
					{
						id: "needs-input-sess",
						workspaceId: "proj1",
						workspaceName: "Proj One",
						title: "Needs input session",
						provider: "claude-code",
						branch: "main",
						status: "needs_input",
						updatedAt: "2026-08-01T00:00:00Z",
						prs: [],
					},
				],
			},
		];
		renderHome(workspaces);

		const titles = screen.getAllByText(/session$/).map((element) => element.textContent);
		expect(titles).toEqual(["Needs input session", "Working session"]);

		await user.click(screen.getByText("Needs input session"));
		expect(navigateMock).toHaveBeenCalledWith({
			to: "/projects/$projectId/sessions/$sessionId",
			params: { projectId: "proj1", sessionId: "needs-input-sess" },
		});
	});
});
