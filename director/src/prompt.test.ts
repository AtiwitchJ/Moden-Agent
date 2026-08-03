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
		expect(prompt).toContain("ao spawn");
	});

	it("makes the Director answer worker terminal questions", () => {
		expect(prompt).toContain("answer_worker");
	});
});
