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
