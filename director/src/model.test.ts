import { describe, expect, it } from "vitest";
import { createDirectorModel } from "./model.js";

describe("createDirectorModel", () => {
	it("uses the configured OpenAI model without dynamic provider imports", () => {
		const model = createDirectorModel("openai:gpt-5", { OPENAI_API_KEY: "test-key" });
		expect(model.lc_namespace).toContain("openai");
	});

	it("requires the provider key before starting the Director", () => {
		expect(() => createDirectorModel("anthropic:claude-sonnet-4-6", {})).toThrow(/ANTHROPIC_API_KEY/);
	});

	it("rejects providers that are not embedded in the Director bundle", () => {
		expect(() => createDirectorModel("ollama:qwen", {})).toThrow(/supports/);
	});
});
