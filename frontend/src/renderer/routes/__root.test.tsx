import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useShell } from "../lib/shell-context";
import { RootComponent } from "./__root";

const { useDaemonStatusMock } = vi.hoisted(() => ({
	useDaemonStatusMock: vi.fn(),
}));

vi.mock("../hooks/useDaemonStatus", () => ({
	useDaemonStatus: (...args: unknown[]) => useDaemonStatusMock(...args),
}));

vi.mock("@tanstack/react-router", async (importOriginal) => {
	const actual = await importOriginal<typeof import("@tanstack/react-router")>();
	return {
		...actual,
		useRouterState: vi.fn(() => ({ location: { pathname: "/" } })),
		Outlet: () => <ShellProbe />,
	};
});

vi.mock("../components/TitlebarNav", () => ({
	TitlebarNav: () => <div data-testid="titlebar-nav">TitlebarNav</div>,
}));

vi.mock("../components/ModeBar", () => ({
	ModeBar: () => <div data-testid="mode-bar">ModeBar</div>,
}));

// Probe component that verifies useShell() works inside root's ShellProvider
function ShellProbe() {
	const { daemonStatus } = useShell();
	return <div data-testid="shell-probe">daemonStatus: {daemonStatus?.state}</div>;
}

describe("RootComponent", () => {
	beforeEach(() => {
		useDaemonStatusMock.mockReturnValue({ state: "ready", port: 8080, message: "" });
	});

	it("provides ShellContext so children can use useShell()", () => {
		const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });

		render(
			<QueryClientProvider client={queryClient}>
				<RootComponent />
			</QueryClientProvider>,
		);

		// Verify root renders its own components
		expect(screen.getByTestId("mode-bar")).toBeInTheDocument();
		expect(screen.getByTestId("titlebar-nav")).toBeInTheDocument();

		// Verify child inside ShellProvider can access context and read daemonStatus
		expect(screen.getByTestId("shell-probe")).toBeInTheDocument();
		expect(screen.getByText(/daemonStatus: ready/)).toBeInTheDocument();
	});
});
