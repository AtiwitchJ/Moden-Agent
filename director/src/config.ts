/**
 * Engine default. Overridden per project via `agentConfig.model`, which the Go
 * adapter forwards as AO_DIRECTOR_MODEL. DeepAgents takes "provider:model-name",
 * so openai:, anthropic:, google:, openrouter:, fireworks:, baseten:, and
 * ollama: are all selectable.
 */
export const DEFAULT_MODEL = "anthropic:claude-sonnet-4-6";

const DEFAULT_MAX_ITERATIONS = 60;

export interface DirectorConfig {
	model: string;
	cardId: string;
	sessionId: string;
	prompt: string;
	maxIterations: number;
}

function required(env: NodeJS.ProcessEnv, key: string): string {
	const value = (env[key] ?? "").trim();
	if (value === "") {
		throw new Error(`${key} is required but was not set`);
	}
	return value;
}

export function loadConfig(env: NodeJS.ProcessEnv): DirectorConfig {
	const cardId = required(env, "AO_DIRECTOR_CARD_ID");
	const sessionId = required(env, "AO_SESSION_ID");

	const model = (env.AO_DIRECTOR_MODEL ?? "").trim() || DEFAULT_MODEL;

	// A malformed budget must fail loudly: silently falling back would hide the
	// misconfiguration behind a budget the operator did not choose.
	const rawMax = (env.AO_DIRECTOR_MAX_ITERATIONS ?? "").trim();
	let maxIterations = DEFAULT_MAX_ITERATIONS;
	if (rawMax !== "") {
		const parsed = Number(rawMax);
		if (!Number.isInteger(parsed) || parsed <= 0) {
			throw new Error(`AO_DIRECTOR_MAX_ITERATIONS must be a positive integer, got ${JSON.stringify(rawMax)}`);
		}
		maxIterations = parsed;
	}

	return { model, cardId, sessionId, prompt: (env.AO_PROMPT ?? "").trim(), maxIterations };
}
