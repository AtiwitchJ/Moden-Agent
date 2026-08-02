import { useMutation, useQuery } from "@tanstack/react-query";
import type { components } from "../../api/schema";
import { apiClient, apiErrorMessage } from "../lib/api-client";

export type WorkCard = components["schemas"]["WorkCardResponse"];
export type RedoCycle = components["schemas"]["RedoCycleResponse"];
export type RedoFinding = components["schemas"]["RedoFindingResponse"];
export type DispatchFailure = components["schemas"]["DispatchFailureResponse"];
export type DirectorStatus = components["schemas"]["DirectorStatusResponse"];

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

export function useWorkCardDispatchFailure(cardId?: string, enabled: boolean = true) {
	return useQuery({
		queryKey: ["workcard", cardId, "dispatch-failure"],
		queryFn: async () => {
			if (!cardId) return undefined;
			const { data, error } = await apiClient.GET("/api/v1/workboard/cards/{cardId}/dispatch-failure", {
				params: { path: { cardId } },
			});
			// A 404 here simply means this card has no recorded dispatch failure;
			// treat that as a quiet absence rather than an error.
			if (error && typeof error === "object" && "status" in error && error.status === 404) {
				return undefined;
			}
			if (error) throw new Error(apiErrorMessage(error, "Could not load dispatch failure."));
			return data as DispatchFailure | undefined;
		},
		enabled: Boolean(cardId) && enabled,
	});
}

export function useDirectorStatus(projectId?: string) {
	return useQuery({
		queryKey: ["workboard", projectId, "director-status"],
		queryFn: async () => {
			if (!projectId) return undefined;
			const { data, error } = await apiClient.GET("/api/v1/projects/{projectId}/workboard/director-status", {
				params: { path: { projectId } },
			});
			if (error) throw new Error(apiErrorMessage(error, "Could not load director status."));
			return data as DirectorStatus | undefined;
		},
		enabled: Boolean(projectId),
	});
}

export function useDispatchProject(projectId?: string) {
	return useMutation({
		mutationFn: async () => {
			if (!projectId) throw new Error("Project is required.");
			const { error } = await apiClient.POST("/api/v1/projects/{projectId}/workboard/dispatch", {
				params: { path: { projectId } },
			});
			if (error) throw new Error(apiErrorMessage(error, "Could not request dispatch."));
		},
	});
}
