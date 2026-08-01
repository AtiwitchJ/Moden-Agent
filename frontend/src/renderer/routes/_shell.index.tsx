import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { CodeHomeComposer } from "../components/CodeHomeComposer";
import { SessionDot } from "../components/SessionDot";
import { useWorkspaceQuery } from "../hooks/useWorkspaceQuery";
import { formatRelativeTime } from "../lib/relative-time";
import { recentSessions } from "../lib/recent-sessions";
import { useUiStore } from "../stores/ui-store";
import { attentionZone } from "../types/workspace";

export const Route = createFileRoute("/_shell/")({
	component: CodeHome,
});

// Code mode's home: a Sessions list (attention-needing sessions first) above
// the always-present CodeHomeComposer. Replaces CEODashboard (relocated to
// components/CEODashboard.tsx, unrouted) — see
// docs/superpowers/specs/2026-08-02-moden-code-home-design.md.
export function CodeHome() {
	const navigate = useNavigate();
	const orgName = useUiStore((s) => s.orgName);
	const workspaceQuery = useWorkspaceQuery();
	const workspaces = workspaceQuery.data ?? [];
	const sessions = recentSessions(workspaces);
	const needsAttention = sessions.filter((session) => attentionZone(session) === "action");
	const rest = sessions.filter((session) => attentionZone(session) !== "action");
	const ordered = [...needsAttention, ...rest];

	return (
		<div className="flex h-full min-h-0 flex-col overflow-y-auto bg-background text-foreground">
			<div className="mx-auto w-full max-w-3xl flex-1 px-6 pt-10">
				<h1 className="text-[21px] font-bold tracking-[-0.025em] text-foreground">Welcome back, {orgName}</h1>

				{ordered.length > 0 && (
					<div className="mt-8">
						<h2 className="text-[13px] font-semibold uppercase tracking-[0.06em] text-passive">Sessions</h2>
						<ul className="mt-3 divide-y divide-border rounded-lg border border-border">
							{ordered.map((session) => (
								<li key={session.id}>
									<button
										className="flex w-full items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-interactive-hover"
										onClick={() =>
											void navigate({
												to: "/projects/$projectId/sessions/$sessionId",
												params: { projectId: session.workspaceId, sessionId: session.id },
											})
										}
										type="button"
									>
										<SessionDot session={session} />
										<span className="min-w-0 flex-1">
											<span className="block truncate text-[13px] font-medium text-foreground">{session.title}</span>
											<span className="block truncate text-[12px] text-passive">{session.workspaceName}</span>
										</span>
										<span className="shrink-0 text-[11px] text-passive">{formatRelativeTime(session.updatedAt)}</span>
									</button>
								</li>
							))}
						</ul>
					</div>
				)}

				{ordered.length === 0 && !workspaceQuery.isLoading && (
					<p className="mt-8 text-[13px] text-passive">No sessions yet — describe what to work on below to start one.</p>
				)}
			</div>

			<CodeHomeComposer />
		</div>
	);
}
