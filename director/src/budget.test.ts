import { describe, expect, it } from "vitest";
import { IterationBudget, RESERVE_ITERATIONS } from "./budget.js";

describe("IterationBudget", () => {
	it("starts unused", () => {
		const b = new IterationBudget(10);
		expect(b.used).toBe(0);
		expect(b.remaining).toBe(10);
	});

	it("counts iterations", () => {
		const b = new IterationBudget(10);
		b.recordIteration();
		b.recordIteration();
		expect(b.used).toBe(2);
		expect(b.remaining).toBe(8);
	});

	it("does not force a block while budget is comfortable", () => {
		const b = new IterationBudget(10);
		b.recordIteration();
		expect(b.shouldForceBlock()).toBe(false);
	});

	it("forces a block once the reserve is reached, before the budget is spent", () => {
		const b = new IterationBudget(10);
		for (let i = 0; i < 10 - RESERVE_ITERATIONS; i++) b.recordIteration();
		expect(b.remaining).toBe(RESERVE_ITERATIONS);
		expect(b.shouldForceBlock()).toBe(true);
		// The whole point: there is still budget left to issue the transition.
		expect(b.remaining).toBeGreaterThan(0);
	});

	it("stays forced past exhaustion and never reports negative remaining", () => {
		const b = new IterationBudget(2);
		for (let i = 0; i < 5; i++) b.recordIteration();
		expect(b.shouldForceBlock()).toBe(true);
		expect(b.remaining).toBe(0);
	});

	it("rejects a non-positive max", () => {
		expect(() => new IterationBudget(0)).toThrow(/positive/);
	});
});
