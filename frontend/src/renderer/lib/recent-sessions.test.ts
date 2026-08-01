import { describe, expect, it } from "vitest";
import { recentSessions } from "./recent-sessions";
import type { WorkspaceSession, WorkspaceSummary } from "../types/workspace";

function session(overrides: Partial<WorkspaceSession> = {}): WorkspaceSession {
	return {
		id: "s1",
		workspaceId: "w1",
		workspaceName: "w1",
		title: "s1",
		provider: "claude-code",
		branch: "main",
		status: "working",
		updatedAt: "2026-08-01T00:00:00Z",
		prs: [],
		...overrides,
	};
}

function workspace(id: string, sessions: WorkspaceSession[]): WorkspaceSummary {
	return { id, name: id, path: `/${id}`, sessions };
}

describe("recentSessions", () => {
	it("flattens sessions across workspaces, newest updatedAt first", () => {
		const workspaces = [
			workspace("w1", [session({ id: "a", updatedAt: "2026-08-01T00:00:00Z" })]),
			workspace("w2", [session({ id: "b", updatedAt: "2026-08-02T00:00:00Z" })]),
		];
		expect(recentSessions(workspaces).map((s) => s.id)).toEqual(["b", "a"]);
	});

	it("excludes merged and terminated sessions", () => {
		const workspaces = [
			workspace("w1", [
				session({ id: "active", status: "working" }),
				session({ id: "merged", status: "merged" }),
				session({ id: "terminated", status: "terminated" }),
			]),
		];
		expect(recentSessions(workspaces).map((s) => s.id)).toEqual(["active"]);
	});

	it("caps to limit", () => {
		const workspaces = [
			workspace("w1", [
				session({ id: "a", updatedAt: "2026-08-01T00:00:00Z" }),
				session({ id: "b", updatedAt: "2026-08-02T00:00:00Z" }),
			]),
		];
		expect(recentSessions(workspaces, 1).map((s) => s.id)).toEqual(["b"]);
	});
});
