import { describe, expect, it } from "vitest";
import { DEFAULT_MODEL, loadConfig } from "./config.js";

const base = { AO_DIRECTOR_CARD_ID: "card-1", AO_SESSION_ID: "sess-1", AO_PROJECT_ID: "proj-1" };

describe("loadConfig", () => {
	it("uses the configured model", () => {
		const cfg = loadConfig({ ...base, AO_DIRECTOR_MODEL: "openrouter:minimax/minimax-m2" });
		expect(cfg.model).toBe("openrouter:minimax/minimax-m2");
	});

	it("falls back to the default model when unset", () => {
		expect(loadConfig(base).model).toBe(DEFAULT_MODEL);
	});

	it("falls back to the default model when blank", () => {
		expect(loadConfig({ ...base, AO_DIRECTOR_MODEL: "   " }).model).toBe(DEFAULT_MODEL);
	});

	it("throws when the card id is missing", () => {
		expect(() => loadConfig({ AO_SESSION_ID: "sess-1" })).toThrow(/AO_DIRECTOR_CARD_ID/);
	});

	it("throws when the session id is missing", () => {
		expect(() => loadConfig({ AO_DIRECTOR_CARD_ID: "card-1" })).toThrow(/AO_SESSION_ID/);
	});

	it("throws when the project id is missing", () => {
		expect(() => loadConfig({ AO_DIRECTOR_CARD_ID: "card-1", AO_SESSION_ID: "sess-1" })).toThrow(/AO_PROJECT_ID/);
	});

	it("defaults maxIterations to 60", () => {
		expect(loadConfig(base).maxIterations).toBe(60);
	});

	it("reads maxIterations when set", () => {
		expect(loadConfig({ ...base, AO_DIRECTOR_MAX_ITERATIONS: "10" }).maxIterations).toBe(10);
	});

	it("rejects a non-numeric maxIterations rather than silently defaulting", () => {
		expect(() => loadConfig({ ...base, AO_DIRECTOR_MAX_ITERATIONS: "abc" })).toThrow(/AO_DIRECTOR_MAX_ITERATIONS/);
	});
});
