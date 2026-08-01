import { useQuery } from "@tanstack/react-query";
import type { components } from "../../api/schema";
import { apiClient, apiErrorMessage } from "../lib/api-client";

export type WorkCard = components["schemas"]["WorkCardResponse"];
export type RedoCycle = components["schemas"]["RedoCycleResponse"];
export type RedoFinding = components["schemas"]["RedoFindingResponse"];

// Shared cache key for the Workboard query.
export const workboardQueryKey = (projectId?: string) =>
	projectId ? (["workboard", projectId] as const) : (["workboard", "global"] as const);

async function fetchWorkboardCards(projectId?: string): Promise<WorkCard[]> {
	if (projectId) {
		const { data, error } = await apiClient.GET("/api/v1/projects/{projectId}/workboard/cards", {
			params: { path: { projectId } },
		});
		if (error) throw new Error(apiErrorMessage(error, "Could not load workboard."));
		return data?.cards ?? [];
	}
	const { data, error } = await apiClient.GET("/api/v1/workboard/cards");
	if (error) throw new Error(apiErrorMessage(error, "Could not load global workboard."));
	return data?.cards ?? [];
}

export function useWorkboardCards(projectId?: string) {
	return useQuery({
		queryKey: workboardQueryKey(projectId),
		queryFn: () => fetchWorkboardCards(projectId),
		enabled: true,
	});
}

export function useWorkCardRedo(cardId?: string, enabled: boolean = true) {
	return useQuery({
		queryKey: ["workcard", cardId, "redo"],
		queryFn: async () => {
			if (!cardId) return [];
			const { data, error } = await apiClient.GET("/api/v1/workboard/cards/{cardId}/redo", {
				params: { path: { cardId } },
			});
			if (error) throw new Error(apiErrorMessage(error, "Could not load redo history."));
			return data?.cycles ?? [];
		},
		enabled: Boolean(cardId) && enabled,
	});
}
