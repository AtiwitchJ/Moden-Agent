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
});
