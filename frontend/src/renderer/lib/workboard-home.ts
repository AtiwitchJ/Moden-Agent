import type { WorkspaceSummary } from "../types/workspace";

export const lastProjectStorageKey = "ao.lastProjectId";

function getLocalStorage() {
	if (typeof window === "undefined" || !window.localStorage) return null;
	return window.localStorage;
}

export function readLastProjectId(): string | null {
	return getLocalStorage()?.getItem(lastProjectStorageKey)?.trim() || null;
}

export function writeLastProjectId(projectId: string): void {
	const id = projectId.trim();
	if (id) getLocalStorage()?.setItem(lastProjectStorageKey, id);
}

/** Code mode home no longer redirects: the global Workboard moved to the
 * Code Manage mode (/manage), so `/` renders the CEO Dashboard directly. */
export function workboardHomeRedirectTarget(
	_workspaces?: readonly WorkspaceSummary[],
	_lastProjectId: string | null = readLastProjectId(),
): null {
	return null;
}

