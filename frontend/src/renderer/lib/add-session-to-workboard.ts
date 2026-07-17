import type { components } from "../../api/schema";
import { apiClient, apiErrorMessage } from "./api-client";
import { canonicalTrackerIssueId, isOrchestratorSession, type WorkspaceSession, type WorkspaceSummary } from "../types/workspace";

type CreateWorkCardRequest = components["schemas"]["CreateWorkCardRequest"];
type WorkCard = components["schemas"]["WorkCardResponse"];

const legacyLabel = "legacy-session";

export function sessionCanJoinWorkboard(session: WorkspaceSession): boolean {
	return !isOrchestratorSession(session) && session.status !== "terminated";
}

export function buildLegacyWorkCardInput(session: WorkspaceSession, workspace: WorkspaceSummary): CreateWorkCardRequest {
	const issueId = canonicalTrackerIssueId(session.issueId);
	const branchNote = session.branch.trim() ? `\n\nBranch: ${session.branch.trim()}` : "";
	return {
		title: session.title.trim() || session.id,
		notes: `Legacy session linked from the Sessions board.${branchNote}`,
		priority: "normal",
		labels: issueId ? [issueId] : [legacyLabel],
		status: "running",
		targetPath: workspace.path,
		agent: session.provider,
		sessionId: session.id,
	};
}

export async function addSessionToWorkboard(session: WorkspaceSession, workspace: WorkspaceSummary): Promise<WorkCard> {
	if (!sessionCanJoinWorkboard(session)) {
		throw new Error("Only active worker sessions can be added to the Workboard.");
	}
	const body = buildLegacyWorkCardInput(session, workspace);
	const { data, error } = await apiClient.POST("/api/v1/projects/{projectId}/workboard/cards", {
		params: { path: { projectId: workspace.id } },
		body,
	});
	if (error) throw new Error(apiErrorMessage(error, "Could not create work card."));
	if (!data?.id) throw new Error("Work card creation returned no card.");
	return data;
}

export function sessionLinkedWorkCard(cards: readonly WorkCard[] | undefined, sessionId: string): WorkCard | undefined {
	return cards?.find((card) => card.sessionId === sessionId);
}
