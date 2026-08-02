import { isOrchestratorSession, sessionIsActive, type WorkspaceSession, type WorkspaceSummary } from "../types/workspace";

/**
 * Every active worker session across every workspace, newest-updated first.
 * Backs the Code-mode sidebar's Recents list and the Home pane's Sessions
 * section — the flat, cross-project view that replaced the project/company
 * tree. Orchestrator sessions (Director/project commanders) are excluded so
 * they don't leak into the Code shell, which is reserved for hands-on worker
 * sessions. The per-project Workboard sidebar and SessionsBoard already
 * filter the same way via workerSessions().
 */
export function recentSessions(workspaces: WorkspaceSummary[], limit = Infinity): WorkspaceSession[] {
	return workspaces
		.flatMap((workspace) => workspace.sessions)
		.filter((session) => sessionIsActive(session) && !isOrchestratorSession(session))
		.sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt))
		.slice(0, limit);
}
