import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ModeBar, activeModeFromPathname } from "./ModeBar";
import { ShellProvider } from "../lib/shell-context";
import * as eventsConnection from "../lib/events-connection";

// Mock router
vi.mock("@tanstack/react-router", () => ({
	useNavigate: () => vi.fn(),
	useRouterState: (opts?: { select?: (state: any) => unknown }) => {
		const state = { location: { pathname: "/" } };
		return opts?.select ? opts.select(state) : state;
	},
}));

// Mock events-connection module
vi.mock("../lib/events-connection", () => ({
	getEventsConnectionState: vi.fn(() => "idle"),
	subscribeEventsConnection: vi.fn(() => vi.fn()),
	setEventsConnectionState: vi.fn(),
}));

const mockDaemonStatus = (state: "starting" | "ready" | "stopped" | "error") => ({
	daemonStatus: { state },
	createProject: vi.fn(),
	removeProject: vi.fn(),
});

const renderWithShell = (daemonState: "starting" | "ready" | "stopped" | "error") => {
	const shellValue = mockDaemonStatus(daemonState);
	return render(
		<ShellProvider value={shellValue}>
			<ModeBar />
		</ShellProvider>,
	);
};

describe("activeModeFromPathname", () => {
	it("maps /manage and nested paths to manage", () => {
		expect(activeModeFromPathname("/manage")).toBe("manage");
		expect(activeModeFromPathname("/manage/anything")).toBe("manage");
	});
	it("maps /work to work", () => {
		expect(activeModeFromPathname("/work")).toBe("work");
	});
	it("maps everything else to code", () => {
		expect(activeModeFromPathname("/")).toBe("code");
		expect(activeModeFromPathname("/projects/p1/sessions/s1")).toBe("code");
		expect(activeModeFromPathname("/workboard")).toBe("code"); // redirects to /manage before render
	});
});

describe("ConnectionIndicator", () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	const getDot = () => document.querySelector('[aria-hidden="true"].rounded-full');

	it("renders Daemon offline with red dot when daemon is not ready", () => {
		renderWithShell("stopped");
		const dot = getDot();
		expect(dot).toBeInTheDocument();
		expect(dot?.className).toContain("bg-destructive");
	});

	it("renders Live with green dot when daemon is ready and SSE is connected", () => {
		vi.mocked(eventsConnection.getEventsConnectionState).mockReturnValue("connected");
		renderWithShell("ready");
		const dot = getDot();
		expect(dot).toBeInTheDocument();
		expect(dot?.className).toContain("bg-success");
	});

	it("renders Reconnecting with amber dot when daemon is ready but SSE is disconnected", () => {
		vi.mocked(eventsConnection.getEventsConnectionState).mockReturnValue("disconnected");
		renderWithShell("ready");
		const dot = getDot();
		expect(dot).toBeInTheDocument();
		expect(dot?.className).toContain("bg-amber");
	});

	it("has aria-live='polite' so screen readers announce state changes", () => {
		vi.mocked(eventsConnection.getEventsConnectionState).mockReturnValue("connected");
		renderWithShell("ready");
		const indicator = document.querySelector('[aria-live="polite"]');
		expect(indicator).toBeInTheDocument();
	});
});
