import { describe, expect, it } from "vitest";
import { inboundInstruction } from "./inbound.js";

describe("inboundInstruction", () => {
	it("keeps a multi-line report intact", () => {
		const report = "Handoff: coding done\nChanged: src/a.ts\nChecks: npm test PASS";
		expect(inboundInstruction(report)).toContain(report);
	});

	it("does not pre-classify the message as a question", () => {
		const out = inboundInstruction("Handoff: coding done");
		expect(out).not.toContain("terminal question");
	});

	it("names both outcomes so a finished phase can advance the card", () => {
		const out = inboundInstruction("Handoff: coding done");
		expect(out).toContain("transition_card");
		expect(out).toContain("answer_worker");
	});

	it("trims surrounding whitespace", () => {
		expect(inboundInstruction("  hi  \n")).toContain("hi");
		expect(inboundInstruction("  hi  \n")).not.toContain("  hi  ");
	});
});
