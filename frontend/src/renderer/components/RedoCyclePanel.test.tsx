import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { RedoCyclePanel } from "./RedoCyclePanel";

const mockCycles = [
	{
		id: "cycle-1",
		cardId: "card-1",
		cycleNumber: 1,
		status: "failed" as const,
		summary: "Lint failure in workboard_store.go",
		source: "testing_gate",
		findings: [
			{
				id: "finding-1",
				cycleId: "cycle-1",
				severity: "P1",
				title: "Fix type mismatch on labels",
				details: "Incompatible slice type",
				status: "open",
				sequence: 1,
				attemptCount: 1,
				fileRefs: [{ file: "backend/store.go", startLine: 42 }],
				createdAt: "2026-07-21T00:00:00Z",
				updatedAt: "2026-07-21T00:00:00Z",
			},
			{
				id: "finding-2",
				cycleId: "cycle-1",
				severity: "P0",
				title: "Syntax error on migration SQL",
				details: "Missing semicolon",
				status: "resolved",
				sequence: 0,
				attemptCount: 1,
				fileRefs: [{ file: "backend/migrations/0034.sql" }],
				createdAt: "2026-07-21T00:00:00Z",
				updatedAt: "2026-07-21T00:00:00Z",
			},
		],
		createdAt: "2026-07-21T00:00:00Z",
		updatedAt: "2026-07-21T00:00:00Z",
	},
];

describe("RedoCyclePanel", () => {
	it("renders redo cycles and prioritizes findings (P0 > P1)", async () => {
		render(<RedoCyclePanel cycles={mockCycles} latestSummary="Cycle 1 failed with 2 findings" />);

		expect(screen.getByText("Redo History (1 cycle)")).toBeInTheDocument();
		expect(screen.getByText("Cycle 1 failed with 2 findings")).toBeInTheDocument();
		expect(screen.getByText("Cycle #1")).toBeInTheDocument();

		// Findings are listed in priority order: P0 first, then P1
		const listItems = screen.getAllByRole("listitem");
		expect(listItems.length).toBe(2);
		expect(listItems[0]).toHaveTextContent("P0");
		expect(listItems[0]).toHaveTextContent("Syntax error on migration SQL");
		expect(listItems[1]).toHaveTextContent("P1");
		expect(listItems[1]).toHaveTextContent("Fix type mismatch on labels");
	});

	it("collapses and expands cycles when toggled", async () => {
		const user = userEvent.setup();
		render(<RedoCyclePanel cycles={mockCycles} />);

		// Open by default
		expect(screen.getByText("Fix type mismatch on labels")).toBeInTheDocument();

		// Toggle to close
		await user.click(screen.getByRole("button", { name: /Cycle #1/i }));
		expect(screen.queryByText("Fix type mismatch on labels")).not.toBeInTheDocument();
	});
});
