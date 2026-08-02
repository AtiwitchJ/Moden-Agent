import { apiClient, apiErrorMessage } from "./api-client";

/** Starts an implementation session directly, using the project's configured
 * worker agent. Code mode uses this path so a prompt is handled by a coding
 * agent rather than by the project's orchestration session. */
export async function spawnWorker(projectId: string, prompt: string): Promise<string> {
	const { data, error, response } = await apiClient.POST("/api/v1/sessions", {
		body: { projectId, kind: "worker", prompt },
	});

	if (error || !data?.session?.id) {
		const message = error
			? apiErrorMessage(error, `Failed to start coding session (${response.status})`)
			: `Failed to start coding session (${response.status})`;
		throw new Error(message);
	}

	return data.session.id;
}
