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

	it("rejects providers that are not embedded and not registered as a custom provider", () => {
		expect(() => createDirectorModel("ollama:qwen", {})).toThrow(/supports/);
	});

	it("builds an OpenAI-compatible client for a registered custom provider", () => {
		const env = {
			AO_DIRECTOR_CUSTOM_PROVIDERS: JSON.stringify({ minimax: { baseUrl: "https://api.minimax.io/v1", apiKey: "sk-cp-test" } }),
		};
		const model = createDirectorModel("minimax:MiniMax-M3", env);
		expect(model.lc_namespace).toContain("openai");
	});

	it("accepts a custom provider with no api key, e.g. a local Ollama endpoint", () => {
		const env = {
			AO_DIRECTOR_CUSTOM_PROVIDERS: JSON.stringify({ ollama: { baseUrl: "http://localhost:11434/v1" } }),
		};
		expect(() => createDirectorModel("ollama:qwen", env)).not.toThrow();
	});

	it("still rejects an unregistered provider even when other custom providers exist", () => {
		const env = {
			AO_DIRECTOR_CUSTOM_PROVIDERS: JSON.stringify({ minimax: { baseUrl: "https://api.minimax.io/v1", apiKey: "k" } }),
		};
		expect(() => createDirectorModel("groq:llama", env)).toThrow(/supports/);
	});

	it("ignores a malformed AO_DIRECTOR_CUSTOM_PROVIDERS rather than crashing the Director on a bad save", () => {
		const env = { AO_DIRECTOR_CUSTOM_PROVIDERS: "{not json" };
		expect(() => createDirectorModel("openai:gpt-5", { ...env, OPENAI_API_KEY: "k" })).not.toThrow();
		expect(() => createDirectorModel("minimax:MiniMax-M3", env)).toThrow(/supports/);
	});
});
