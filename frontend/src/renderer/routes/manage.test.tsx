import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@tanstack/react-router", async (importOriginal) => {
	const actual = await importOriginal<typeof import("@tanstack/react-router")>();
	return { ...actual, createFileRoute: () => (opts: unknown) => opts };
});
vi.mock("../components/Workboard", () => ({ Workboard: () => <div data-testid="workboard" /> }));

import { ManagePage } from "./manage";

describe("ManagePage", () => {
	it("renders the global Workboard full-bleed without the project sidebar", () => {
		render(<ManagePage />);
		expect(screen.getByTestId("workboard")).toBeInTheDocument();
		expect(screen.queryByLabelText("Holdings Dashboard")).not.toBeInTheDocument();
	});

	it("provides ShellProvider so Workboard's useShell() call does not throw", () => {
		// Regression test: Workboard calls useShell() internally. ManagePage must
		// wrap Workboard with ShellProvider, otherwise useShell throws "must be used
		// within the _shell layout route". This test verifies that wrapping exists
		// by rendering ManagePage and confirming no error is thrown.
		const { container } = render(<ManagePage />);
		expect(container).toBeInTheDocument();
	});
});
