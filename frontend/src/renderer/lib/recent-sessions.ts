import { sessionIsActive, type WorkspaceSession, type WorkspaceSummary } from "../types/workspace";

/**
 * Every active session across every workspace, newest-updated first. Backs
 * the Code-mode sidebar's Recents list and the Home pane's Sessions section —
 * the flat, cross-project view that replaced the project/company tree.
 */
export function recentSessions(workspaces: WorkspaceSummary[], limit = Infinity): WorkspaceSession[] {
	return workspaces
		.flatMap((workspace) => workspace.sessions)
		.filter(sessionIsActive)
		.sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt))
		.slice(0, limit);
}
