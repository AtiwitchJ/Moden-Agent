import { describe, expect, it } from "vitest";
import type { WorkspaceSession, WorkspaceSummary } from "../types/workspace";
import { buildLegacyWorkCardInput, sessionCanJoinWorkboard } from "./add-session-to-workboard";

const workspace: WorkspaceSummary = {
	id: "proj-1",
	name: "proj-1",
	path: "/repo/proj-1",
	sessions: [],
};

const worker: WorkspaceSession = {
	id: "sess-1",
	workspaceId: "proj-1",
	workspaceName: "proj-1",
	title: "Fix checkout",
	issueId: "github:ENG-42",
	provider: "codex",
	kind: "worker",
	branch: "feat/checkout",
	status: "working",
	updatedAt: "2026-01-01T00:00:00Z",
	prs: [],
};

describe("buildLegacyWorkCardInput", () => {
	it("maps session fields into a running card create payload", () => {
		expect(buildLegacyWorkCardInput(worker, workspace)).toEqual({
			title: "Fix checkout",
			notes: "Legacy session linked from the Sessions board.\n\nBranch: feat/checkout",
			priority: "normal",
			labels: ["github:ENG-42"],
			status: "running",
			targetPath: "/repo/proj-1",
			agent: "codex",
			sessionId: "sess-1",
		});
	});

	it("rejects orchestrator and terminated sessions", () => {
		expect(sessionCanJoinWorkboard({ ...worker, kind: "orchestrator" })).toBe(false);
		expect(sessionCanJoinWorkboard({ ...worker, status: "terminated" })).toBe(false);
	});
});
