import { ChatAnthropic } from "@langchain/anthropic";
import { ChatOpenAI } from "@langchain/openai";

function required(env: NodeJS.ProcessEnv, key: string): string {
	const value = env[key]?.trim();
	if (!value) throw new Error(`${key} is required for the configured Director model`);
	return value;
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
	switch (provider.trim().toLowerCase()) {
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
		default:
			throw new Error(`Director bundle supports openai:, anthropic:, and openrouter: models; got ${JSON.stringify(provider)}`);
	}
}
