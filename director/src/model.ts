import { ChatAnthropic } from "@langchain/anthropic";
import { ChatOpenAI } from "@langchain/openai";

function required(env: NodeJS.ProcessEnv, key: string): string {
	const value = env[key]?.trim();
	if (!value) throw new Error(`${key} is required for the configured Director model`);
	return value;
}

interface CustomProvider {
	baseUrl: string;
	apiKey?: string;
}

/** Reads the operator-configured custom-provider registry (any OpenAI-compatible
 * endpoint beyond the three built-ins — MiniMax, a local Ollama, etc.), written
 * by Global Settings' Director section into AO_DIRECTOR_CUSTOM_PROVIDERS as one
 * JSON blob shared by every Director project. A malformed blob degrades to "no
 * custom providers configured" rather than crashing the Director on a bad save
 * — the same tolerance ao workboard card handoff uses for one bad audit row. */
function readCustomProviders(env: NodeJS.ProcessEnv): Record<string, CustomProvider> {
	const raw = (env.AO_DIRECTOR_CUSTOM_PROVIDERS ?? "").trim();
	if (raw === "") return {};
	try {
		const parsed: unknown = JSON.parse(raw);
		if (typeof parsed !== "object" || parsed === null) return {};
		return parsed as Record<string, CustomProvider>;
	} catch {
		return {};
	}
}

/**
 * Resolves the providers this first-party bundle ships directly. Passing a
 * model instance (rather than a provider:model string) avoids LangChain's
 * runtime dynamic import, which cannot resolve from AO's embedded flat module
 * archive after installation.
 */
export function createDirectorModel(model: string, env: NodeJS.ProcessEnv): ChatOpenAI | ChatAnthropic {
	const [provider, ...name] = model.split(":");
	const modelName = name.join(":").trim();
	if (!provider || !modelName) {
		throw new Error(`AO_DIRECTOR_MODEL must be "provider:model-name", got ${JSON.stringify(model)}`);
	}
	const providerId = provider.trim().toLowerCase();
	switch (providerId) {
		case "openai":
			return new ChatOpenAI({ model: modelName, apiKey: required(env, "OPENAI_API_KEY") });
		case "anthropic":
			return new ChatAnthropic({ model: modelName, apiKey: required(env, "ANTHROPIC_API_KEY") });
		case "openrouter":
			return new ChatOpenAI({
				model: modelName,
				apiKey: required(env, "OPENROUTER_API_KEY"),
				configuration: { baseURL: "https://openrouter.ai/api/v1" },
			});
		default: {
			const custom = readCustomProviders(env)[providerId];
			if (custom) {
				// A local endpoint (e.g. Ollama) does not check the key, but the
				// OpenAI client still requires a non-empty string to construct.
				return new ChatOpenAI({
					model: modelName,
					apiKey: custom.apiKey?.trim() || "unused",
					configuration: { baseURL: custom.baseUrl },
				});
			}
			const known = Object.keys(readCustomProviders(env));
			const knownSuffix = known.length > 0 ? `, or a registered custom provider (${known.join(", ")})` : "";
			throw new Error(
				`Director bundle supports openai:, anthropic:, and openrouter: models${knownSuffix}; got ${JSON.stringify(provider)}`,
			);
		}
	}
}
