import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

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

describe("ManagePage", () => {
	beforeEach(() => {
		useWorkboardCardsMock.mockReturnValue({ data: [], isError: false });
		useWorkspaceQueryMock.mockReturnValue({ data: [] });
	});

	it("renders Workboard with ShellProvider so useShell() does not throw", () => {
		// Regression test: Workboard calls useShell() to read daemonStatus. ManagePage
		// must wrap Workboard with ShellProvider; without it, useShell throws
		// "must be used within the _shell layout route". This test renders the real
		// Workboard component (not mocked) so the hook call is exercised and verifies
		// ManagePage's ShellProvider satisfies it.
		render(
			<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
				<ManagePage />
			</QueryClientProvider>,
		);

		// Verify Workboard rendered (it shows "Workboard" in the heading).
		expect(screen.getByText("Workboard")).toBeInTheDocument();

		// Verify no error boundary: if useShell() threw, React would show the error
		// or a fallback; absence of the error text confirms ShellProvider worked.
		expect(screen.queryByText(/must be used within/i)).not.toBeInTheDocument();
	});
});
