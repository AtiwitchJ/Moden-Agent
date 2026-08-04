import type { components } from "../../api/schema";

export const WORKBOARD_ORCHESTRATOR_AGENT = "hermes";
export const WORKBOARD_DIRECTOR_HARNESS = "director";
export const DEFAULT_WORKBOARD_CODING_AGENT = "codex";

export const DEFAULT_WORKBOARD_CONFIG: components["schemas"]["WorkboardConfig"] = {
	wipLimit: 4,
};

export function isWorkboardEnabled(config?: components["schemas"]["ProjectConfig"] | null): boolean {
	return config?.workboard !== undefined;
}

/** The env var the Director's bundled model factory reads for a given engine
 * string. Mirrors createDirectorModel in director/src/model.ts, which throws at
 * startup when the key is absent — so a wrong name here is a Director that will
 * not boot. Keep the two in step. */
export function directorKeyEnvVar(model: string): string {
	const provider = model.split(":")[0]?.trim().toLowerCase();
	if (provider === "openai") return "OPENAI_API_KEY";
	if (provider === "openrouter") return "OPENROUTER_API_KEY";
	return "ANTHROPIC_API_KEY"; // the Director's default engine is anthropic:
}

export const DIRECTOR_KEY_ENV_VARS = ["ANTHROPIC_API_KEY", "OPENAI_API_KEY", "OPENROUTER_API_KEY"];
