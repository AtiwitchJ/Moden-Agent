import { describe, expect, it } from "vitest";
import { directorSystemPrompt } from "./prompt.js";

describe("directorSystemPrompt", () => {
	const prompt = directorSystemPrompt("card-42");

	it("names the card it owns", () => {
		expect(prompt).toContain("card-42");
	});

	it("uses the current transition command", () => {
		expect(prompt).toContain("ao workboard card transition");
	});

	it("does not reference the stale status command", () => {
		expect(prompt).not.toContain("ao workboard status");
	});

	it("instructs blocking before the budget is exhausted", () => {
		expect(prompt).toMatch(/blocked/i);
		expect(prompt).toMatch(/budget/i);
	});

	it("tells the agent to delegate implementation rather than code directly", () => {
		expect(prompt).toContain("spawn_worker");
	});

	it("makes the Director treat one-shot workers as blocking and handoff-only", () => {
		expect(prompt).toContain("spawn_worker");
		expect(prompt).toContain("one-shot");
		expect(prompt).not.toContain("answer_worker");
	});

	it("requires durable phase handoffs before delegation continues", () => {
		expect(prompt).toContain("workboard card handoff card-42");
		expect(prompt).toContain("handoffs");
		expect(prompt).toContain("git diff");
	});

	it("keeps Git workflow automation within destructive-action guardrails", () => {
		expect(prompt).toContain("git reset --hard");
		expect(prompt).toContain("force-push");
		expect(prompt).toContain("dirty registered worktree");
	});
});
