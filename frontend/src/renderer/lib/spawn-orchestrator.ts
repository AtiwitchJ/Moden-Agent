import { apiClient, apiErrorMessage } from "./api-client";

/** Spawn the project's orchestrator session via the daemon API. When clean is
 *  true the daemon first tears down any active orchestrator for the project, then
 *  re-spawns one on the canonical branch (reattaching the existing branch).
 *  When prompt is set, the daemon delivers it as the orchestrator's first
 *  message — immediately if one was already running, or once a freshly
 *  spawned process's PTY is actually ready (readiness-polled server-side, not
 *  a client-side guess), so callers should NOT also call the /send endpoint
 *  themselves for this message. */
export async function spawnOrchestrator(projectId: string, clean = false, prompt?: string): Promise<string> {
	const { data, error, response } = await apiClient.POST("/api/v1/orchestrators", {
		body: { projectId, clean, ...(prompt ? { prompt } : {}) },
	});

	if (error || !data?.orchestrator?.id) {
		const message = error
			? apiErrorMessage(error, `Failed to spawn orchestrator (${response.status})`)
			: `Failed to spawn orchestrator (${response.status})`;
		throw new Error(message);
	}

	return data.orchestrator.id;
}
