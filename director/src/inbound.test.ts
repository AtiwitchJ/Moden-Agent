import { describe, expect, it } from "vitest";
import { extractMessages, inboundInstruction, MESSAGE_END_SENTINEL } from "./inbound.js";

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

describe("extractMessages", () => {
	it("returns the whole input when it ends with the sentinel", () => {
		const input = "hello world" + MESSAGE_END_SENTINEL;
		const { messages, remainder } = extractMessages(input);
		expect(messages).toEqual(["hello world"]);
		expect(remainder).toBe("");
	});

	it("buffers an input that has no sentinel yet", () => {
		const input = "partial chunk";
		const { messages, remainder } = extractMessages(input);
		expect(messages).toEqual([]);
		expect(remainder).toBe("partial chunk");
	});

	it("returns multiple whole messages and the trailing remainder", () => {
		const input = "first" + MESSAGE_END_SENTINEL + "second" + MESSAGE_END_SENTINEL + "third";
		const { messages, remainder } = extractMessages(input);
		expect(messages).toEqual(["first", "second"]);
		expect(remainder).toBe("third");
	});

	it("handles a chunk that arrives mid-sentinel across a chunk boundary", () => {
		// The first chunk ends mid-sentinel. The second chunk completes it.
		const partialSentinel = MESSAGE_END_SENTINEL.slice(0, 5);
		const firstChunk = "report" + partialSentinel;
		const { messages: a, remainder: r1 } = extractMessages(firstChunk);
		expect(a).toEqual([]);
		expect(r1).toBe(firstChunk);

		const secondChunk = MESSAGE_END_SENTINEL.slice(5) + "next" + MESSAGE_END_SENTINEL;
		const { messages: b, remainder: r2 } = extractMessages(r1 + secondChunk);
		expect(b).toEqual(["report", "next"]);
		expect(r2).toBe("");
	});
});
