export const lastProjectStorageKey = "ao.lastProjectId";

function getLocalStorage() {
	if (typeof window === "undefined" || !window.localStorage) return null;
	return window.localStorage;
}

export function writeLastProjectId(projectId: string): void {
	const id = projectId.trim();
	if (id) getLocalStorage()?.setItem(lastProjectStorageKey, id);
}
