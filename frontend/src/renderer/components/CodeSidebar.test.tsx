import { QueryClientProvider, QueryClient } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { WorkspaceSummary } from "../types/workspace";

const { navigateMock, pathnameMock } = vi.hoisted(() => ({ navigateMock: vi.fn(), pathnameMock: vi.fn(() => "/") }));

vi.mock("@tanstack/react-router", async (importOriginal) => {
	const actual = await importOriginal<typeof import("@tanstack/react-router")>();
	return {
		...actual,
		useNavigate: () => navigateMock,
		useRouterState: () => pathnameMock(),
	};
});

import { SidebarProvider } from "./ui/sidebar";
import { CodeSidebar, CODE_HOME_COMPOSER_INPUT_ID } from "./CodeSidebar";

function renderSidebar(workspaces: WorkspaceSummary[]) {
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={queryClient}>
			<SidebarProvider>
				<input id={CODE_HOME_COMPOSER_INPUT_ID} aria-label="composer stand-in" />
				<CodeSidebar workspaces={workspaces} />
			</SidebarProvider>
		</QueryClientProvider>,
	);
}

beforeEach(() => {
	navigateMock.mockReset();
	pathnameMock.mockReset().mockReturnValue("/");
});

describe("CodeSidebar", () => {
	it("clicking New navigates home when on another route, and focuses the composer input", async () => {
		const user = userEvent.setup();
		pathnameMock.mockReturnValue("/prs");
		renderSidebar([]);

		await user.click(screen.getByRole("button", { name: "New" }));

		expect(navigateMock).toHaveBeenCalledWith({ to: "/" });
		await waitFor(() => expect(document.activeElement).toBe(screen.getByLabelText("composer stand-in")));
	});

	it("clicking New while already home skips navigate and just focuses the composer input", async () => {
		const user = userEvent.setup();
		pathnameMock.mockReturnValue("/");
		renderSidebar([]);

		await user.click(screen.getByRole("button", { name: "New" }));

		expect(navigateMock).not.toHaveBeenCalled();
		await waitFor(() => expect(document.activeElement).toBe(screen.getByLabelText("composer stand-in")));
	});

	it("lists sessions beneath their project and navigates to the clicked one", async () => {
		const user = userEvent.setup();
		const workspaces: WorkspaceSummary[] = [
			{
				id: "proj1",
				name: "Proj One",
				path: "/proj1",
				sessions: [
					{
						id: "sess1",
						workspaceId: "proj1",
						workspaceName: "Proj One",
						title: "Fix the thing",
						provider: "claude-code",
						branch: "main",
						status: "working",
						updatedAt: "2026-08-02T00:00:00Z",
						prs: [],
					},
				],
			},
		];
		renderSidebar(workspaces);

		expect(screen.getByText("Proj One")).toBeInTheDocument();
		expect(screen.getByText("Fix the thing")).toBeInTheDocument();
		await user.click(screen.getByText("Fix the thing"));

		expect(navigateMock).toHaveBeenCalledWith({
			to: "/projects/$projectId/sessions/$sessionId",
			params: { projectId: "proj1", sessionId: "sess1" },
		});
	});

	it("collapses and expands a project's sessions without navigating away", async () => {
		const user = userEvent.setup();
		const workspaces: WorkspaceSummary[] = [
			{
				id: "proj1",
				name: "Proj One",
				path: "/proj1",
				sessions: [
					{
						id: "sess1",
						workspaceId: "proj1",
						workspaceName: "Proj One",
						title: "Fix the thing",
						provider: "claude-code",
						branch: "main",
						status: "working",
						updatedAt: "2026-08-02T00:00:00Z",
						prs: [],
					},
				],
			},
		];
		renderSidebar(workspaces);

		await user.click(screen.getByRole("button", { name: "Toggle project Proj One" }));
		expect(screen.queryByText("Fix the thing")).not.toBeInTheDocument();
		expect(navigateMock).not.toHaveBeenCalled();
		await user.click(screen.getByRole("button", { name: "Toggle project Proj One" }));
		expect(screen.getByText("Fix the thing")).toBeInTheDocument();
	});

	it("shows an empty state when there are no projects", () => {
		renderSidebar([]);
		expect(screen.getByText("No projects yet.")).toBeInTheDocument();
	});

	it("More menu opens and navigates to Settings", async () => {
		const user = userEvent.setup();
		renderSidebar([]);

		await user.click(screen.getByRole("button", { name: "More" }));
		await user.click(await screen.findByText("Settings"));

		expect(navigateMock).toHaveBeenCalledWith({ to: "/settings" });
	});
});
