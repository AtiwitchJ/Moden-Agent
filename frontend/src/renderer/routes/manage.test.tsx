import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { DaemonStatus } from "../lib/daemon-status";

const { useWorkboardCardsMock, useWorkspaceQueryMock } = vi.hoisted(() => ({
	useWorkboardCardsMock: vi.fn(),
	useWorkspaceQueryMock: vi.fn(),
}));

vi.mock("@tanstack/react-router", async (importOriginal) => {
	const actual = await importOriginal<typeof import("@tanstack/react-router")>();
	return { ...actual, createFileRoute: () => (opts: unknown) => opts };
});

vi.mock("../hooks/useWorkboardQuery", () => ({
	workboardQueryKey: (projectId?: string) => (projectId ? ["workboard", projectId] : ["workboard"]),
	useWorkboardCards: (...args: unknown[]) => useWorkboardCardsMock(...args),
	useWorkCardRedo: () => ({ data: [], isError: false }),
}));

vi.mock("../hooks/useWorkspaceQuery", () => ({ useWorkspaceQuery: (...args: unknown[]) => useWorkspaceQueryMock(...args) }));

vi.mock("../lib/api-client", async (importOriginal) => {
	const actual = await importOriginal<typeof import("../lib/api-client")>();
	return {
		...actual,
		apiClient: {
			...actual.apiClient,
			GET: vi.fn(),
			PATCH: vi.fn(),
			POST: vi.fn(),
		},
	};
});

vi.mock("../components/TerminalPane", () => ({ TerminalPane: () => <div>terminal pane</div> }));

import { ManagePage } from "./manage";
import { ShellProvider } from "../lib/shell-context";

describe("ManagePage", () => {
	beforeEach(() => {
		useWorkboardCardsMock.mockReturnValue({ data: [], isError: false });
		useWorkspaceQueryMock.mockReturnValue({ data: [] });
	});

	it("renders Workboard", () => {
		// ManagePage is now simplified to just render Workboard; ShellProvider
		// must be provided by a parent (normally root) for useShell() to resolve.
		const mockDaemonStatus: DaemonStatus = { state: "ready", port: 8080, message: "" };
		render(
			<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
				<ShellProvider
					value={{
						daemonStatus: mockDaemonStatus,
						createProject: async () => {
							throw new Error("createProject not available in test");
						},
					}}
				>
					<ManagePage />
				</ShellProvider>
			</QueryClientProvider>,
		);

		// Verify Workboard rendered (it shows "Workboard" in the heading).
		expect(screen.getByText("Workboard")).toBeInTheDocument();
	});
});
