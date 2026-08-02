# Moden Code Home Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Code mode's home (CEO Dashboard) and its project/company-tree sidebar with a chat-first layout — a slim New/Recents/More sidebar and a Home pane whose bottom composer is the primary way to start a session (pick or create a project, type a prompt, submit).

**Architecture:** Pure frontend change, composed entirely from existing daemon endpoints (`POST /projects`, `POST /orchestrators`, `POST /sessions/{id}/send`). `CEODashboard` is relocated (not deleted) from `routes/_shell.index.tsx` into `components/CEODashboard.tsx`, unrouted. `_shell.index.tsx` becomes the new `CodeHome`. `_shell.tsx` swaps its `<Sidebar>` render for a new `<CodeSidebar>`. `ShellContextValue` gains a typed session-id return on `createProject` and a shared `removeProject`, so both new components and the relocated project-settings page can start/stop projects without duplicating `_shell.tsx`'s cache-update logic.

**Tech Stack:** React 19, TanStack Router/Query, shadcn primitives (`Sidebar`, `Select`, `DropdownMenu`), existing `apiClient` (openapi-typescript generated), Vitest + Testing Library.

## Global Constraints

- Design spec: `docs/superpowers/specs/2026-08-02-moden-code-home-design.md` — read it for the locked product decisions this plan implements.
- No backend/daemon changes. Every composer action reuses an existing endpoint.
- Company/HQ concept: untouched, out of scope (tracked as its own future spec per the design doc).
- `CEODashboard` code must be relocated, not deleted — moving files is fine, deleting the component/its tests is not.
- Follow DESIGN.md: build from shadcn primitives where one fits; the existing `Sidebar` primitive set (`components/ui/sidebar.tsx`) is the fit for `CodeSidebar`.
- All existing tests must keep passing: `cd frontend && npm run test && npm run typecheck`.
- "New" only focuses the composer — it must not open a separate creation dialog.

---

### Task 1: `recentSessions` helper

**Files:**
- Create: `frontend/src/renderer/lib/recent-sessions.ts`
- Test: `frontend/src/renderer/lib/recent-sessions.test.ts`

**Interfaces:**
- Produces: `recentSessions(workspaces: WorkspaceSummary[], limit?: number): WorkspaceSession[]` — active sessions (`sessionIsActive`) flattened across every workspace, newest `updatedAt` first, optionally capped. Consumed by Tasks 6 and 8 (`CodeSidebar`, `CodeHome`).

- [ ] **Step 1: Write the failing test**

```ts
// frontend/src/renderer/lib/recent-sessions.test.ts
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run --config vite.renderer.config.ts src/renderer/lib/recent-sessions.test.ts`
Expected: FAIL — `Cannot find module './recent-sessions'`.

- [ ] **Step 3: Implement**

```ts
// frontend/src/renderer/lib/recent-sessions.ts
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run --config vite.renderer.config.ts src/renderer/lib/recent-sessions.test.ts`
Expected: PASS (3/3).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/renderer/lib/recent-sessions.ts frontend/src/renderer/lib/recent-sessions.test.ts
git commit -m "feat(ui): add recentSessions helper for the cross-project session list"
```

---

### Task 2: `formatRelativeTime` helper

**Files:**
- Create: `frontend/src/renderer/lib/relative-time.ts`
- Test: `frontend/src/renderer/lib/relative-time.test.ts`

**Interfaces:**
- Produces: `formatRelativeTime(iso: string, now?: Date): string` — compact relative time ("5 minutes ago"), falling back to a locale date past a year. Consumed by Tasks 6 and 8.

- [ ] **Step 1: Write the failing test**

```ts
// frontend/src/renderer/lib/relative-time.test.ts
import { describe, expect, it } from "vitest";
import { formatRelativeTime } from "./relative-time";

describe("formatRelativeTime", () => {
	const now = new Date("2026-08-02T12:00:00Z");

	it("returns 'just now' for under 5 seconds", () => {
		expect(formatRelativeTime("2026-08-02T11:59:58Z", now)).toBe("just now");
	});

	it("formats minutes", () => {
		expect(formatRelativeTime("2026-08-02T11:55:00Z", now)).toBe("5 minutes ago");
	});

	it("formats hours", () => {
		expect(formatRelativeTime("2026-08-02T09:00:00Z", now)).toBe("3 hours ago");
	});

	it("formats days", () => {
		expect(formatRelativeTime("2026-07-31T12:00:00Z", now)).toBe("2 days ago");
	});

	it("falls back to a locale date past a year", () => {
		expect(formatRelativeTime("2020-01-01T00:00:00Z", now)).toBe(new Date("2020-01-01T00:00:00Z").toLocaleDateString());
	});
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run --config vite.renderer.config.ts src/renderer/lib/relative-time.test.ts`
Expected: FAIL — `Cannot find module './relative-time'`.

- [ ] **Step 3: Implement**

```ts
// frontend/src/renderer/lib/relative-time.ts
const UNITS: { limit: number; divisor: number; unit: Intl.RelativeTimeFormatUnit }[] = [
	{ limit: 60, divisor: 1, unit: "second" },
	{ limit: 3600, divisor: 60, unit: "minute" },
	{ limit: 86400, divisor: 3600, unit: "hour" },
	{ limit: 604800, divisor: 86400, unit: "day" },
	{ limit: 2629800, divisor: 604800, unit: "week" },
	{ limit: 31557600, divisor: 2629800, unit: "month" },
];

const formatter = new Intl.RelativeTimeFormat("en", { numeric: "auto" });

/**
 * Compact relative time for session rows ("5 minutes ago", "2 days ago").
 * Falls back to a locale date once past a year.
 * ponytail: English only — add locale support when a second locale is needed.
 */
export function formatRelativeTime(iso: string, now: Date = new Date()): string {
	const then = Date.parse(iso);
	if (Number.isNaN(then)) return "";
	const diffSeconds = Math.round((now.getTime() - then) / 1000);
	if (diffSeconds < 5) return "just now";
	for (const { limit, divisor, unit } of UNITS) {
		if (diffSeconds < limit) return formatter.format(-Math.round(diffSeconds / divisor), unit);
	}
	return new Date(then).toLocaleDateString();
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run --config vite.renderer.config.ts src/renderer/lib/relative-time.test.ts`
Expected: PASS (5/5).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/renderer/lib/relative-time.ts frontend/src/renderer/lib/relative-time.test.ts
git commit -m "feat(ui): add formatRelativeTime helper"
```

---

### Task 3: Relocate `CEODashboard` out of the route file

**Files:**
- Create: `frontend/src/renderer/components/CEODashboard.tsx`
- Create: `frontend/src/renderer/components/CEODashboard.test.tsx`
- Modify: `frontend/src/renderer/routes/_shell.index.tsx`
- Delete: `frontend/src/renderer/routes/_shell.index.test.tsx`

**Interfaces:**
- Produces: `CEODashboard` exported from `components/CEODashboard.tsx` (same component, same behavior — relocated only). `/` still renders it after this task; Task 8 replaces that with `CodeHome`.

This task is a pure move: copy `CEODashboard`'s current body verbatim, fix the one import path that changes because the file moved, and update the two files that reference it. No behavior change — the app must look and test identically after this task.

- [ ] **Step 1: Read the current file to copy verbatim**

Run: `cat frontend/src/renderer/routes/_shell.index.tsx` — the full current content is the source for Step 2 (only the `CreateProjectFlow` import path changes: `../components/Sidebar` → `./Sidebar`, since the file is moving from `routes/` into `components/`; every other import path is unchanged because `routes/` and `components/` are both direct children of `renderer/`).

- [ ] **Step 2: Create the relocated component**

Create `frontend/src/renderer/components/CEODashboard.tsx` with this content (identical to the current `_shell.index.tsx` body, minus the `createFileRoute`/`Route` export, with the one import path fixed):

```tsx
// frontend/src/renderer/components/CEODashboard.tsx
import { useNavigate } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Building2, Plus, ArrowRight, Briefcase, Pencil, Terminal } from "lucide-react";
import { useUiStore } from "../stores/ui-store";
import { useCompaniesQuery, companiesQueryKey } from "../hooks/useCompaniesQuery";
import { useWorkspaceQuery } from "../hooks/useWorkspaceQuery";
import { apiClient, apiErrorMessage } from "../lib/api-client";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "./ui/card";
import { DashboardSubhead } from "./DashboardSubhead";
import { CreateProjectFlow } from "./Sidebar";
import { useShell } from "../lib/shell-context";
import { groupWorkspacesByCompany, sessionIsActive } from "../types/workspace";
import { MigrationPopup } from "./MigrationPopup";
import { HQSection } from "./HQSection";
import { HeartbeatPauseSwitch } from "./HeartbeatPauseSwitch";
import { cn } from "../lib/utils";

// Relocated verbatim from routes/_shell.index.tsx when Code mode's home
// became CodeHome (see docs/superpowers/specs/2026-08-02-moden-code-home-design.md).
// Kept, not deleted — unrouted, reachable only if a future route wires it up.
export function CEODashboard() {
	const companiesQuery = useCompaniesQuery();
	const workspacesQuery = useWorkspaceQuery();
	const queryClient = useQueryClient();
	const navigate = useNavigate();
	const { createProject } = useShell();
	const orgName = useUiStore((s) => s.orgName);
	const setOrgName = useUiStore((s) => s.setOrgName);
	// null = not editing; string = the draft name being typed.
	const [orgNameDraft, setOrgNameDraft] = useState<string | null>(null);
	const [newCompanyName, setNewCompanyName] = useState("");
	const [isCreating, setIsCreating] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const [watchLiveError, setWatchLiveError] = useState<string | null>(null);

	const watchLive = async (companyId: string) => {
		setWatchLiveError(null);
		const { data, error } = await apiClient.GET("/api/v1/org/overview");
		if (error) {
			setWatchLiveError(apiErrorMessage(error, "Could not load org overview"));
			return;
		}
		const overview = data?.overview;
		const company = overview?.companies.find((c) => c.id === companyId);
		const activeProject = company?.projects.find((p) => p.activeSessions > 0);
		const ids = [
			overview?.holdingHq?.orchestratorSessionId,
			company?.hq?.orchestratorSessionId,
			activeProject?.orchestratorSessionId,
		].filter((id): id is string => Boolean(id));
		navigate({ to: "/terminals", search: { sessions: ids.join(",") } });
	};

	const createCompanyMutation = useMutation({
		mutationFn: async (name: string) => {
			const { data, error } = await apiClient.POST("/api/v1/companies", {
				body: { name },
			});
			if (error) throw new Error(apiErrorMessage(error, "Failed to create company"));
			return data?.company;
		},
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: companiesQueryKey });
			setNewCompanyName("");
			setIsCreating(false);
		},
		onError: (err) => {
			setError(err instanceof Error ? err.message : "An error occurred");
		},
	});

	const handleCreateCompany = (e: React.FormEvent) => {
		e.preventDefault();
		if (!newCompanyName.trim()) return;
		setError(null);
		createCompanyMutation.mutate(newCompanyName.trim());
	};

	const companies = companiesQuery.data ?? [];
	const workspaces = workspacesQuery.data ?? [];
	const companyGroups = groupWorkspacesByCompany(workspaces, companies);

	const headerActions = isCreating ? (
		<form onSubmit={handleCreateCompany} className="flex items-center gap-2">
			<Input
				autoFocus
				placeholder="Company name..."
				value={newCompanyName}
				onChange={(e) => setNewCompanyName(e.target.value)}
				className="w-48"
				disabled={createCompanyMutation.isPending}
			/>
			<Button type="submit" disabled={createCompanyMutation.isPending || !newCompanyName.trim()}>
				Save
			</Button>
			<Button type="button" variant="ghost" onClick={() => setIsCreating(false)}>
				Cancel
			</Button>
		</form>
	) : (
		<>
			<HeartbeatPauseSwitch />
			<Button variant="outline" onClick={() => setIsCreating(true)} className="gap-2">
				<Plus size={16} />
				Add Company
			</Button>
			<CreateProjectFlow onCreateProject={createProject}>
				{({ disabled, choosePath, label }) => (
					<Button onClick={choosePath} disabled={disabled} className="gap-2">
						<Plus size={16} />
						{label === "New project" ? "Add Project" : label}
					</Button>
				)}
			</CreateProjectFlow>
		</>
	);

	return (
		<div className="flex h-full min-h-0 flex-col overflow-y-auto bg-background text-foreground">
			<MigrationPopup />
			<DashboardSubhead
				title={
					orgNameDraft === null ? (
						<span className="group/orgname inline-flex items-center gap-2">
							{orgName} CEO Dashboard
							<button
								aria-label="Rename organization"
								className="text-passive opacity-0 transition-opacity hover:text-foreground focus-visible:opacity-100 group-hover/orgname:opacity-100"
								onClick={() => setOrgNameDraft(orgName)}
								type="button"
							>
								<Pencil size={14} aria-hidden="true" />
							</button>
						</span>
					) : (
						<input
							aria-label="Organization name"
							autoFocus
							className="w-64 rounded-md border border-input bg-transparent px-2 py-0.5 text-[17px] font-bold outline-none focus-visible:ring-2 focus-visible:ring-ring/60"
							value={orgNameDraft}
							onChange={(e) => setOrgNameDraft(e.target.value)}
							onBlur={() => {
								setOrgName(orgNameDraft);
								setOrgNameDraft(null);
							}}
							onKeyDown={(e) => {
								if (e.key === "Enter") {
									setOrgName(orgNameDraft);
									setOrgNameDraft(null);
								} else if (e.key === "Escape") {
									setOrgNameDraft(null);
								}
							}}
						/>
					)
				}
				subtitle="Overview of all companies and operations"
				actions={headerActions}
			/>

			<div className="mx-auto w-full max-w-6xl space-y-8 px-[18px] pb-8 pt-6">
				{error && (
					<div className="rounded-md border border-destructive/30 bg-destructive/10 p-4 text-destructive">
						{error}
					</div>
				)}
				{watchLiveError && (
					<div className="rounded-md border border-destructive/30 bg-destructive/10 p-4 text-destructive">
						{watchLiveError}
					</div>
				)}

				<HQSection scope={{ kind: "holding" }} />

				{companiesQuery.isLoading || workspacesQuery.isLoading ? (
					<div className="text-passive">Loading holdings data...</div>
				) : (
					<div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
						{companyGroups.map((group) => {
							const isUnassigned = group.id === "unassigned";
							const title = group.name;
							const projectCount = group.workspaces.length;
							const activeSessions = group.workspaces.reduce((acc, ws) => acc + ws.sessions.filter(s => sessionIsActive(s)).length, 0);

							return (
								<Card
									key={group.id}
									className={cn("transition-colors", !isUnassigned && "cursor-pointer hover:border-accent/50")}
									onClick={() => {
										if (!isUnassigned) {
											navigate({ to: "/companies/$companyId", params: { companyId: group.id } });
										}
									}}
								>
									<CardHeader className="pb-2">
										<div className="flex items-center justify-between">
											<div className="p-2 bg-accent/10 rounded-lg">
												{isUnassigned ? <Briefcase className="text-accent" size={24} /> : <Building2 className="text-accent" size={24} />}
											</div>
											{!isUnassigned && <ArrowRight className="text-passive" size={20} />}
										</div>
										<CardTitle className="text-xl mt-4 text-foreground">{title}</CardTitle>
										<CardDescription>
											{isUnassigned ? "Projects without a company" : `Company ID: ${group.id}`}
										</CardDescription>
									</CardHeader>
									<CardContent>
										<div className="flex gap-4 mt-4">
											<div>
												<div className="text-2xl font-semibold text-foreground">{projectCount}</div>
												<div className="text-xs text-passive uppercase tracking-wider font-semibold">Products</div>
											</div>
											<div>
												<div className="text-2xl font-semibold text-foreground">{activeSessions}</div>
												<div className="text-xs text-passive uppercase tracking-wider font-semibold">Active Agents</div>
											</div>
										</div>
										{!isUnassigned && (
											<Button
												aria-label={`Watch ${group.name} live`}
												className="mt-4 w-full gap-2"
												onClick={(e) => {
													e.stopPropagation();
													void watchLive(group.id);
												}}
												size="sm"
												variant="outline"
											>
												<Terminal size={14} />
												Watch Live
											</Button>
										)}
									</CardContent>
								</Card>
							);
						})}
					</div>
				)}
			</div>
		</div>
	);
}
```

- [ ] **Step 3: Move the test file verbatim**

Create `frontend/src/renderer/components/CEODashboard.test.tsx` with the exact content of the current `frontend/src/renderer/routes/_shell.index.test.tsx`, changing only line 26's import:

```tsx
import { CEODashboard } from "./CEODashboard";
```

(every other line — mocks, `renderDashboard`, both `describe` blocks — is unchanged; the mocked paths `../lib/api-client`, `../components/MigrationPopup`, `../components/HQSection`, `../components/HeartbeatPauseSwitch`, `../lib/shell-context` are all still correct since `components/` is a sibling of `routes/`, same depth under `renderer/`).

- [ ] **Step 4: Delete the old test file**

```bash
rm frontend/src/renderer/routes/_shell.index.test.tsx
```

- [ ] **Step 5: Shrink the route file to just re-export**

Replace the full contents of `frontend/src/renderer/routes/_shell.index.tsx`:

```tsx
// frontend/src/renderer/routes/_shell.index.tsx
import { createFileRoute } from "@tanstack/react-router";
import { CEODashboard } from "../components/CEODashboard";

// Temporary: still renders the relocated CEODashboard. Task 8 replaces this
// with CodeHome (see docs/superpowers/plans/2026-08-02-moden-code-home.md).
export const Route = createFileRoute("/_shell/")({
	component: CEODashboard,
});
```

- [ ] **Step 6: Run tests to verify nothing broke**

Run: `cd frontend && npm run test && npm run typecheck`
Expected: PASS, same total test count as before this task (one test file moved, none added or removed).

- [ ] **Step 7: Commit**

```bash
git add frontend/src/renderer/components/CEODashboard.tsx frontend/src/renderer/components/CEODashboard.test.tsx frontend/src/renderer/routes/_shell.index.tsx
git rm frontend/src/renderer/routes/_shell.index.test.tsx
git commit -m "refactor(ui): relocate CEODashboard out of the route file"
```

---

### Task 4: Shared context — typed `createProject` result + shared `removeProject`

**Files:**
- Modify: `frontend/src/renderer/lib/shell-context.ts`
- Modify: `frontend/src/renderer/routes/_shell.tsx`
- Modify: `frontend/src/renderer/routes/__root.tsx`
- Modify: `frontend/src/renderer/components/Sidebar.tsx`
- Modify: `frontend/src/renderer/components/Sidebar.test.tsx`

**Interfaces:**
- Produces: `useShell().createProject(...)` now resolves `Promise<{ projectId: string; sessionId: string }>` (was `Promise<void>`). `useShell().removeProject(projectId: string): Promise<void>` is new. Consumed by Task 7 (`CodeHomeComposer`, needs the session id to send the first message) and Task 10 (`ProjectSettingsForm`, needs `removeProject`).

This is a type-following-reality change: `_shell.tsx`'s `createProject` already knows the new session's id before it navigates — it just wasn't returning it. No new runtime behavior for existing callers; they already `await` the call and ignore the resolved value.

- [ ] **Step 1: Widen `ShellContextValue`**

In `frontend/src/renderer/lib/shell-context.ts`, replace the type:

```ts
export type ShellContextValue = {
	daemonStatus: ReturnType<typeof useDaemonStatus>;
	createProject: (input: {
		path: string;
		workerAgent: string;
		orchestratorAgent: string;
		trackerIntake?: any;
		companyId?: string;
		asWorkspace?: boolean;
	}) => Promise<{ projectId: string; sessionId: string }>;
	removeProject: (projectId: string) => Promise<void>;
};
```

- [ ] **Step 2: Return the session info from `_shell.tsx`'s `createProject`, provide `removeProject`**

In `frontend/src/renderer/routes/_shell.tsx`, inside the `createProject` callback's success path (the `try` block after `spawnOrchestrator`), change:

```ts
			try {
				const sessionId = await spawnOrchestrator(workspace.id);
				await queryClient.invalidateQueries({ queryKey: workspaceQueryKey });
				void navigate({
					to: "/projects/$projectId/sessions/$sessionId",
					params: { projectId: workspace.id, sessionId },
				});
				return { projectId: workspace.id, sessionId };
			} catch (spawnError) {
```

(only the added `return` line changes; the `catch` block is unchanged — it still throws, so the function's only non-throwing exit now carries the return value).

Then update the render to pass `removeProject` through the shell provider too:

```tsx
		<ShellProvider value={{ daemonStatus, createProject, removeProject }}>
```

(`removeProject` is already defined above via `useCallback` in this file — this just adds it to the provided value; no change to its own implementation.)

- [ ] **Step 3: Add a `removeProject` stub to root's provider**

In `frontend/src/renderer/routes/__root.tsx`, extend the `shellContextValue` memo:

```tsx
	const shellContextValue = useMemo(
		() => ({
			daemonStatus,
			createProject: async () => {
				throw new Error("createProject is not available in this context");
			},
			removeProject: async () => {
				throw new Error("removeProject is not available in this context");
			},
		}),
		[daemonStatus],
	);
```

- [ ] **Step 4: Update `Sidebar.tsx`'s prop type**

In `frontend/src/renderer/components/Sidebar.tsx`, `SidebarProps.onCreateProject`'s type (around line 93) changes from `Promise<void>` to match:

```ts
	onCreateProject: (input: { path: string } & CreateProjectAgentSelection) => Promise<{ projectId: string; sessionId: string }>;
```

Nothing else in `Sidebar.tsx` needs to change — its own internal `createProject` wrapper (inside `CreateProjectFlow`, ~line 903) just `await`s and doesn't use the resolved value, so it stays `Promise<void>`-returning itself and remains valid regardless of what `onCreateProject` resolves to.

- [ ] **Step 5: Fix the ripple in `Sidebar.test.tsx`**

In `frontend/src/renderer/components/Sidebar.test.tsx`, line 62, widen the local type:

```ts
type CreateProjectHandler = (input: { path: string; workerAgent: string; orchestratorAgent: string }) => Promise<{
	projectId: string;
	sessionId: string;
}>;
```

Then replace every `.mockResolvedValue(undefined) as CreateProjectHandler` with `.mockResolvedValue({ projectId: "p1", sessionId: "s1" }) as CreateProjectHandler` (6 occurrences: lines 68, 378, 403, 432, 482, 542). Use a single `replace_all`-style edit — the resolved value isn't asserted on anywhere in this file, so this doesn't change what any test checks.

- [ ] **Step 6: Run tests to verify the ripple is fully resolved**

Run: `cd frontend && npm run typecheck && npx vitest run --config vite.renderer.config.ts src/renderer/components/Sidebar.test.tsx`
Expected: typecheck clean, `Sidebar.test.tsx` fully passing (same test count as before this task).

- [ ] **Step 7: Run the full suite**

Run: `cd frontend && npm run test`
Expected: PASS, same total count as end of Task 3 (no tests added or removed by this task — pure type change).

- [ ] **Step 8: Commit**

```bash
git add frontend/src/renderer/lib/shell-context.ts frontend/src/renderer/routes/_shell.tsx frontend/src/renderer/routes/__root.tsx frontend/src/renderer/components/Sidebar.tsx frontend/src/renderer/components/Sidebar.test.tsx
git commit -m "refactor(ui): createProject returns session info, add shared removeProject"
```

---

### Task 5: Extract `SessionDot` into its own component

**Files:**
- Create: `frontend/src/renderer/components/SessionDot.tsx`
- Modify: `frontend/src/renderer/components/Sidebar.tsx`

**Interfaces:**
- Produces: `SessionDot({ session: WorkspaceSession })` exported from its own file. Consumed by Task 6 (`CodeSidebar`) and Task 8 (`CodeHome`), and by `Sidebar.tsx` (unchanged behavior, just relocated).

Pure extraction — no behavior change, so no new test (the existing `Sidebar.test.tsx` coverage that exercises status-dot rendering continues to pass unchanged since the visual output is identical).

- [ ] **Step 1: Create the shared component**

```tsx
// frontend/src/renderer/components/SessionDot.tsx
import { attentionZone, type WorkspaceSession } from "../types/workspace";
import { cn } from "../lib/utils";

/**
 * 6px status dot mirroring the board's attention-zone language. Shared by
 * every compact session row: the old project sidebar, CodeSidebar's Recents,
 * and CodeHome's Sessions list.
 */
export function SessionDot({ session }: { session: WorkspaceSession }) {
	const zone = attentionZone(session);
	return (
		<span
			aria-hidden="true"
			className={cn(
				"mt-px h-1.5 w-1.5 shrink-0 rounded-full",
				zone === "working" && "animate-status-pulse bg-working",
				zone === "action" &&
					(session.status === "ci_failed" || session.status === "stalled" ? "bg-error" : "bg-warning"),
				zone === "pending" && "bg-passive",
				zone === "merge" && "bg-success",
				zone === "done" && "bg-passive",
			)}
		/>
	);
}
```

- [ ] **Step 2: Update `Sidebar.tsx` to use it**

In `frontend/src/renderer/components/Sidebar.tsx`:
1. Delete the local `SessionDot` function definition (currently ~lines 121-139, right after the `noDragStyle` const).
2. Add an import: `import { SessionDot } from "./SessionDot";`
3. Leave every call site (`<SessionDot session={session} />`) exactly as-is — same component name, same props, now imported instead of locally defined.

- [ ] **Step 3: Run tests to verify nothing broke**

Run: `cd frontend && npm run test && npm run typecheck`
Expected: PASS, same total count as end of Task 4.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/renderer/components/SessionDot.tsx frontend/src/renderer/components/Sidebar.tsx
git commit -m "refactor(ui): extract SessionDot into its own component"
```

---

### Task 6: `CodeSidebar` — New / Recents / More

**Files:**
- Modify: `frontend/src/renderer/stores/ui-store.ts`
- Create: `frontend/src/renderer/components/CodeSidebar.tsx`
- Test: `frontend/src/renderer/components/CodeSidebar.test.tsx`

**Interfaces:**
- Consumes: `recentSessions` (Task 1), `formatRelativeTime` (Task 2), `SessionDot` (Task 5), shadcn `Sidebar`/`DropdownMenu` primitives, `useUiStore` (theme, and the new `requestComposerFocus` signal below).
- Produces: `CodeSidebar({ workspaces: WorkspaceSummary[] })`. Task 9 mounts it in `_shell.tsx`.

CodeSidebar's New button lives in a different part of the tree than the composer (sidebar vs. route outlet — not parent/child), so it can't just call a ref. Rather than reach across via a DOM id (fragile: depends on a global id string staying in sync between two unrelated files, and a bare `document.getElementById` + `requestAnimationFrame` poke), this uses the same mechanism the codebase already uses for cross-component UI signals: a counter in `useUiStore` (see `restartingProjectIds`/`setProjectRestarting` for the existing pattern this follows). CodeSidebar bumps it; CodeHomeComposer's focus effect (Task 7) depends on it, so it re-fires on every bump — including the very first render, which is what gives the composer its focus-on-mount behavior too, with no separate effect needed for that case.

- [ ] **Step 1: Extend `ui-store.ts`**

In `frontend/src/renderer/stores/ui-store.ts`, add to `UiState`:

```ts
	focusComposerSignal: number;
	requestComposerFocus: () => void;
```

Add to the store's initial state (alongside `restartingProjectIds: new Set<string>()`):

```ts
	focusComposerSignal: 0,
```

Add the action (alongside `setProjectRestarting`):

```ts
	requestComposerFocus: () => set((state) => ({ focusComposerSignal: state.focusComposerSignal + 1 })),
```

- [ ] **Step 2: Write the failing test**

```tsx
// frontend/src/renderer/components/CodeSidebar.test.tsx
import { QueryClientProvider, QueryClient } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import type { WorkspaceSummary } from "../types/workspace";

const { navigateMock, pathnameMock, requestComposerFocusMock } = vi.hoisted(() => ({
	navigateMock: vi.fn(),
	pathnameMock: vi.fn(() => "/"),
	requestComposerFocusMock: vi.fn(),
}));

vi.mock("@tanstack/react-router", async (importOriginal) => {
	const actual = await importOriginal<typeof import("@tanstack/react-router")>();
	return {
		...actual,
		useNavigate: () => navigateMock,
		useRouterState: () => pathnameMock(),
	};
});

vi.mock("../stores/ui-store", async (importOriginal) => {
	const actual = await importOriginal<typeof import("../stores/ui-store")>();
	return {
		...actual,
		useUiStore: (selector: (s: { theme: string; toggleTheme: () => void; requestComposerFocus: () => void }) => unknown) =>
			selector({ theme: "dark", toggleTheme: vi.fn(), requestComposerFocus: requestComposerFocusMock }),
	};
});

import { SidebarProvider } from "./ui/sidebar";
import { CodeSidebar } from "./CodeSidebar";

function renderSidebar(workspaces: WorkspaceSummary[]) {
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={queryClient}>
			<SidebarProvider>
				<CodeSidebar workspaces={workspaces} />
			</SidebarProvider>
		</QueryClientProvider>,
	);
}

describe("CodeSidebar", () => {
	it("clicking New navigates home when on another route, and requests composer focus", async () => {
		const user = userEvent.setup();
		pathnameMock.mockReturnValue("/prs");
		renderSidebar([]);

		await user.click(screen.getByRole("button", { name: "New" }));

		expect(navigateMock).toHaveBeenCalledWith({ to: "/" });
		expect(requestComposerFocusMock).toHaveBeenCalledTimes(1);
	});

	it("clicking New while already home skips navigate but still requests composer focus", async () => {
		const user = userEvent.setup();
		pathnameMock.mockReturnValue("/");
		renderSidebar([]);

		await user.click(screen.getByRole("button", { name: "New" }));

		expect(navigateMock).not.toHaveBeenCalled();
		expect(requestComposerFocusMock).toHaveBeenCalledTimes(1);
	});

	it("lists recent sessions across workspaces and navigates to the clicked one", async () => {
		const user = userEvent.setup();
		const workspaces: WorkspaceSummary[] = [
			{
				id: "proj1",
				name: "Proj One",
				path: "/proj1",
				sessions: [
					{
						id: "sess1",
						workspaceId: "proj1",
						workspaceName: "Proj One",
						title: "Fix the thing",
						provider: "claude-code",
						branch: "main",
						status: "working",
						updatedAt: "2026-08-02T00:00:00Z",
						prs: [],
					},
				],
			},
		];
		renderSidebar(workspaces);

		expect(screen.getByText("Fix the thing")).toBeInTheDocument();
		await user.click(screen.getByText("Fix the thing"));

		expect(navigateMock).toHaveBeenCalledWith({
			to: "/projects/$projectId/sessions/$sessionId",
			params: { projectId: "proj1", sessionId: "sess1" },
		});
	});

	it("shows an empty state when there are no recent sessions", () => {
		renderSidebar([]);
		expect(screen.getByText("No sessions yet.")).toBeInTheDocument();
	});

	it("More menu opens and navigates to Settings", async () => {
		const user = userEvent.setup();
		renderSidebar([]);

		await user.click(screen.getByRole("button", { name: "More" }));
		await user.click(await screen.findByText("Settings"));

		expect(navigateMock).toHaveBeenCalledWith({ to: "/settings" });
	});
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run --config vite.renderer.config.ts src/renderer/components/CodeSidebar.test.tsx`
Expected: FAIL — `Cannot find module './CodeSidebar'`.

- [ ] **Step 3: Implement**

```tsx
// frontend/src/renderer/components/CodeSidebar.tsx
import { useNavigate, useRouterState } from "@tanstack/react-router";
import { GitPullRequest, Moon, Plus, Settings, Sun, Terminal } from "lucide-react";
import { recentSessions } from "../lib/recent-sessions";
import { formatRelativeTime } from "../lib/relative-time";
import { cn } from "../lib/utils";
import { useUiStore } from "../stores/ui-store";
import type { WorkspaceSummary } from "../types/workspace";
import aoLogo from "../assets/ao-logo.png";
import { SessionDot } from "./SessionDot";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "./ui/dropdown-menu";
import {
	Sidebar as SidebarRoot,
	SidebarContent,
	SidebarFooter,
	SidebarGroup,
	SidebarGroupContent,
	SidebarGroupLabel,
	SidebarHeader,
	SidebarMenu,
	SidebarMenuButton,
	SidebarMenuItem,
	SidebarTrigger,
} from "./ui/sidebar";

const RECENTS_LIMIT = 20;
const isMac = typeof navigator !== "undefined" && /Mac|iPod|iPhone|iPad/.test(navigator.userAgent);

export type CodeSidebarProps = {
	workspaces: WorkspaceSummary[];
};

// Replaces the project/company tree in Code mode: a flat New / Recents / More
// nav modeled on the Claude Code desktop app. Project/company selection moved
// into CodeHomeComposer's picker and the New flow — see
// docs/superpowers/specs/2026-08-02-moden-code-home-design.md.
export function CodeSidebar({ workspaces }: CodeSidebarProps) {
	const navigate = useNavigate();
	const pathname = useRouterState({ select: (state) => state.location.pathname });
	const theme = useUiStore((s) => s.theme);
	const toggleTheme = useUiStore((s) => s.toggleTheme);
	const requestComposerFocus = useUiStore((s) => s.requestComposerFocus);
	const recents = recentSessions(workspaces, RECENTS_LIMIT);

	// New only ever focuses the composer — never a separate dialog. The
	// composer isn't a child of this sidebar (it lives in the route outlet),
	// so "focus it" is a ui-store signal, not a ref: navigate home first if
	// elsewhere, then bump the signal CodeHomeComposer's effect watches.
	const goNew = () => {
		if (pathname !== "/") void navigate({ to: "/" });
		requestComposerFocus();
	};

	return (
		<SidebarRoot collapsible="icon" className="border-border top-14 h-[calc(100svh-3.5rem)]!">
			<SidebarHeader className="gap-0 p-0 pl-2.5 pr-[7px] pt-3.5 group-data-[collapsible=icon]:px-1.5">
				<div className="flex shrink-0 items-center gap-1 px-2 pb-[18px] group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0 group-data-[collapsible=icon]:pb-2">
					<button
						aria-label="Home"
						className="flex h-9 min-w-0 flex-1 items-center gap-2.5 rounded-[5px] px-1.5 transition-colors hover:bg-interactive-hover group-data-[collapsible=icon]:size-9 group-data-[collapsible=icon]:flex-none group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:rounded-lg group-data-[collapsible=icon]:px-0"
						onClick={() => void navigate({ to: "/" })}
						type="button"
					>
						<img alt="" aria-hidden="true" className="h-[22px] w-[22px] shrink-0 rounded-[6px] object-cover" src={aoLogo} />
						<span className="min-w-0 flex-1 truncate text-left text-[14px] font-bold tracking-[-0.015em] text-foreground group-data-[collapsible=icon]:hidden">
							Modern Agent
						</span>
					</button>
					{!isMac && (
						<SidebarTrigger className="size-[18px] shrink-0 rounded-[4px] p-0 text-passive hover:bg-interactive-hover hover:text-foreground group-data-[collapsible=icon]:hidden [&_svg]:size-[15px]" />
					)}
				</div>
			</SidebarHeader>

			<SidebarContent className="gap-0 pl-2.5 pr-[7px] group-data-[collapsible=icon]:items-center group-data-[collapsible=icon]:px-1.5">
				<SidebarGroup className="p-0">
					<SidebarMenu className="gap-0.5">
						<SidebarMenuItem>
							<SidebarMenuButton
								className="h-9 gap-2.5 rounded-[5px] px-2 text-[13px] font-medium text-foreground hover:bg-interactive-hover"
								onClick={goNew}
								tooltip="New"
							>
								<Plus aria-hidden="true" className="size-4" />
								<span className="group-data-[collapsible=icon]:hidden">New</span>
							</SidebarMenuButton>
						</SidebarMenuItem>
					</SidebarMenu>
				</SidebarGroup>

				<SidebarGroup className="mt-2 p-0 group-data-[collapsible=icon]:hidden">
					<SidebarGroupLabel className="h-auto rounded-none px-2 pb-2 text-[10.5px] font-semibold uppercase tracking-[0.09em] text-passive">
						Recents
					</SidebarGroupLabel>
					<SidebarGroupContent>
						<SidebarMenu className="gap-0">
							{recents.length === 0 ? (
								<p className="px-2 py-1.5 text-[12px] text-passive">No sessions yet.</p>
							) : (
								recents.map((session) => (
									<SidebarMenuItem key={session.id}>
										<SidebarMenuButton
											className="h-auto items-start gap-2 rounded-[4px] px-2 py-1.5"
											onClick={() =>
												void navigate({
													to: "/projects/$projectId/sessions/$sessionId",
													params: { projectId: session.workspaceId, sessionId: session.id },
												})
											}
										>
											<SessionDot session={session} />
											<span className="min-w-0 flex-1">
												<span className="block truncate text-[12px] text-foreground">{session.title}</span>
												<span className="block text-[11px] text-passive">{formatRelativeTime(session.updatedAt)}</span>
											</span>
										</SidebarMenuButton>
									</SidebarMenuItem>
								))
							)}
						</SidebarMenu>
					</SidebarGroupContent>
				</SidebarGroup>
			</SidebarContent>

			<SidebarFooter className="mt-auto gap-0 border-t border-border p-[7px] group-data-[collapsible=icon]:items-center group-data-[collapsible=icon]:px-1.5">
				<DropdownMenu>
					<DropdownMenuTrigger asChild>
						<button
							aria-label="More"
							className={cn(
								"flex w-full items-center justify-start gap-2.5 rounded-md p-2 text-[13px] font-medium text-passive transition-colors hover:bg-interactive-hover hover:text-foreground [&_svg]:size-[15px] [&_svg]:text-passive",
								"group-data-[collapsible=icon]:size-9 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:p-0",
							)}
							type="button"
						>
							<Settings aria-hidden="true" />
							<span className="group-data-[collapsible=icon]:hidden">More</span>
						</button>
					</DropdownMenuTrigger>
					<DropdownMenuContent align="start" className="w-56 min-w-0" side="top">
						<DropdownMenuItem onSelect={toggleTheme}>
							{theme === "dark" ? <Sun aria-hidden="true" /> : <Moon aria-hidden="true" />}
							{theme === "dark" ? "Light mode" : "Dark mode"}
						</DropdownMenuItem>
						<DropdownMenuSeparator />
						<DropdownMenuItem onSelect={() => void navigate({ to: "/prs" })}>
							<GitPullRequest aria-hidden="true" />
							Pull requests
						</DropdownMenuItem>
						<DropdownMenuItem onSelect={() => void navigate({ to: "/terminals", search: { sessions: "" } })}>
							<Terminal aria-hidden="true" />
							Live Terminals
						</DropdownMenuItem>
						<DropdownMenuSeparator />
						<DropdownMenuItem onSelect={() => void navigate({ to: "/settings" })}>
							<Settings aria-hidden="true" />
							Settings
						</DropdownMenuItem>
					</DropdownMenuContent>
				</DropdownMenu>
			</SidebarFooter>
		</SidebarRoot>
	);
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run --config vite.renderer.config.ts src/renderer/components/CodeSidebar.test.tsx`
Expected: PASS (5/5).

- [ ] **Step 5: Run typecheck**

Run: `cd frontend && npm run typecheck`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/renderer/stores/ui-store.ts frontend/src/renderer/components/CodeSidebar.tsx frontend/src/renderer/components/CodeSidebar.test.tsx
git commit -m "feat(ui): add CodeSidebar (New / Recents / More)"
```

---

### Task 7: `CodeHomeComposer`

**Files:**
- Create: `frontend/src/renderer/components/CodeHomeComposer.tsx`
- Test: `frontend/src/renderer/components/CodeHomeComposer.test.tsx`

**Interfaces:**
- Consumes: `useShell().createProject` (Task 4's widened return type), `spawnOrchestrator`, `CreateProjectFlow` + `type CreateProjectAgentSelection` (existing), `useUiStore`'s `focusComposerSignal` (Task 6).
- Produces: `CodeHomeComposer()` — no props. Consumed by Task 8 (`CodeHome`).

- [ ] **Step 1: Write the failing test**

```tsx
// frontend/src/renderer/components/CodeHomeComposer.test.tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

const { navigateMock, postMock, spawnOrchestratorMock, createProjectMock, focusSignalMock } = vi.hoisted(() => ({
	navigateMock: vi.fn(),
	postMock: vi.fn(),
	spawnOrchestratorMock: vi.fn(),
	createProjectMock: vi.fn(),
	focusSignalMock: vi.fn(() => 0),
}));

vi.mock("@tanstack/react-router", async (importOriginal) => {
	const actual = await importOriginal<typeof import("@tanstack/react-router")>();
	return { ...actual, useNavigate: () => navigateMock };
});

vi.mock("../lib/api-client", () => ({
	apiClient: { POST: postMock },
	apiErrorMessage: (error: unknown, fallback = "Request failed") => (error instanceof Error ? error.message : fallback),
}));

vi.mock("../lib/spawn-orchestrator", () => ({ spawnOrchestrator: spawnOrchestratorMock }));
vi.mock("../lib/shell-context", () => ({ useShell: () => ({ createProject: createProjectMock }) }));
vi.mock("../hooks/useWorkspaceQuery", () => ({
	useWorkspaceQuery: () => ({
		data: [{ id: "proj1", name: "Proj One", path: "/proj1", sessions: [] }],
	}),
}));
vi.mock("../stores/ui-store", () => ({
	useUiStore: (selector: (s: { focusComposerSignal: number }) => unknown) => selector({ focusComposerSignal: focusSignalMock() }),
}));

import { CodeHomeComposer } from "./CodeHomeComposer";

function renderComposer() {
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	const view = render(
		<QueryClientProvider client={queryClient}>
			<CodeHomeComposer />
		</QueryClientProvider>,
	);
	return {
		...view,
		rerenderComposer: () =>
			view.rerender(
				<QueryClientProvider client={queryClient}>
					<CodeHomeComposer />
				</QueryClientProvider>,
			),
	};
}

describe("CodeHomeComposer", () => {
	it("focuses the prompt input on mount", () => {
		renderComposer();
		expect(screen.getByLabelText("Prompt")).toHaveFocus();
	});

	it("re-focuses the prompt input whenever focusComposerSignal bumps (New clicked while already home)", () => {
		const { rerenderComposer } = renderComposer();
		const input = screen.getByLabelText("Prompt");
		input.blur();
		expect(input).not.toHaveFocus();

		focusSignalMock.mockReturnValue(1);
		rerenderComposer();

		expect(input).toHaveFocus();
	});

	it("existing project: spawns a session, sends the prompt, and navigates", async () => {
		const user = userEvent.setup();
		spawnOrchestratorMock.mockResolvedValue("sess1");
		postMock.mockResolvedValue({ data: {}, error: undefined });
		renderComposer();

		await user.click(screen.getByLabelText("Project"));
		await user.click(await screen.findByText("Proj One"));
		await user.type(screen.getByLabelText("Prompt"), "hello there");
		await user.click(screen.getByRole("button", { name: "Send" }));

		await waitFor(() => expect(spawnOrchestratorMock).toHaveBeenCalledWith("proj1"));
		expect(postMock).toHaveBeenCalledWith("/api/v1/sessions/{sessionId}/send", {
			params: { path: { sessionId: "sess1" } },
			body: { message: "hello there" },
		});
		await waitFor(() =>
			expect(navigateMock).toHaveBeenCalledWith({
				to: "/projects/$projectId/sessions/$sessionId",
				params: { projectId: "proj1", sessionId: "sess1" },
			}),
		);
	});
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run --config vite.renderer.config.ts src/renderer/components/CodeHomeComposer.test.tsx`
Expected: FAIL — `Cannot find module './CodeHomeComposer'`.

- [ ] **Step 3: Implement**

```tsx
// frontend/src/renderer/components/CodeHomeComposer.tsx
import { useNavigate } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { useWorkspaceQuery } from "../hooks/useWorkspaceQuery";
import { apiClient, apiErrorMessage } from "../lib/api-client";
import { useShell } from "../lib/shell-context";
import { spawnOrchestrator } from "../lib/spawn-orchestrator";
import { useUiStore } from "../stores/ui-store";
import type { CreateProjectAgentSelection } from "./CreateProjectAgentSheet";
import { CreateProjectFlow } from "./Sidebar";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./ui/select";

const NEW_PROJECT_VALUE = "__new_project__";

// Primary session-creation surface for Code mode's home: pick a project (or
// create one), type a first prompt, submit — spawns a session and sends the
// prompt as its first message. See
// docs/superpowers/specs/2026-08-02-moden-code-home-design.md.
export function CodeHomeComposer() {
	const navigate = useNavigate();
	const { createProject } = useShell();
	const workspaceQuery = useWorkspaceQuery();
	const workspaces = workspaceQuery.data ?? [];
	const [projectId, setProjectId] = useState<string | null>(null);
	const [draft, setDraft] = useState("");
	const [error, setError] = useState<string | null>(null);
	const [isSubmitting, setIsSubmitting] = useState(false);
	const inputRef = useRef<HTMLInputElement>(null);
	const focusComposerSignal = useUiStore((s) => s.focusComposerSignal);

	// Fires on mount (giving the composer its default focus-on-load) and again
	// every time CodeSidebar's New button bumps the signal — the one shared
	// path for "focus the composer," instead of a separate mount-only effect.
	useEffect(() => {
		inputRef.current?.focus();
	}, [focusComposerSignal]);

	const sendFirstMessage = async (sessionId: string) => {
		const message = draft.trim();
		if (!message) return;
		const { error: sendError } = await apiClient.POST("/api/v1/sessions/{sessionId}/send", {
			params: { path: { sessionId } },
			body: { message },
		});
		if (sendError) {
			throw new Error(apiErrorMessage(sendError, "Session started, but the first message failed to send"));
		}
	};

	const onCreateProject = async (selection: CreateProjectAgentSelection & { path: string }) => {
		setError(null);
		setIsSubmitting(true);
		try {
			const result = await createProject(selection);
			await sendFirstMessage(result.sessionId);
			setDraft("");
			return result;
		} catch (err) {
			setError(err instanceof Error ? err.message : "Could not create project");
			throw err;
		} finally {
			setIsSubmitting(false);
		}
	};

	const submitExisting = async () => {
		if (!projectId) return;
		setError(null);
		setIsSubmitting(true);
		try {
			const sessionId = await spawnOrchestrator(projectId);
			await sendFirstMessage(sessionId);
			setDraft("");
			void navigate({ to: "/projects/$projectId/sessions/$sessionId", params: { projectId, sessionId } });
		} catch (err) {
			setError(err instanceof Error ? err.message : "Could not start session");
		} finally {
			setIsSubmitting(false);
		}
	};

	return (
		<CreateProjectFlow onCreateProject={onCreateProject}>
			{({ choosePath, disabled: pickerBusy }) => (
				<form
					className="sticky bottom-0 flex shrink-0 items-center gap-2 border-t border-border bg-background p-3"
					onSubmit={(event) => {
						event.preventDefault();
						if (!draft.trim() || isSubmitting || pickerBusy) return;
						void submitExisting();
					}}
				>
					<Select
						onValueChange={(value) => {
							if (value === NEW_PROJECT_VALUE) {
								choosePath();
								return;
							}
							setProjectId(value);
						}}
						value={projectId ?? ""}
					>
						<SelectTrigger aria-label="Project" className="w-48 shrink-0">
							<SelectValue placeholder="Choose a project" />
						</SelectTrigger>
						<SelectContent>
							{workspaces.map((workspace) => (
								<SelectItem key={workspace.id} value={workspace.id}>
									{workspace.name}
								</SelectItem>
							))}
							<SelectItem value={NEW_PROJECT_VALUE}>New project…</SelectItem>
						</SelectContent>
					</Select>
					<Input
						aria-label="Prompt"
						className="h-9 flex-1"
						disabled={isSubmitting || pickerBusy}
						onChange={(event) => setDraft(event.target.value)}
						placeholder="Describe what to work on…"
						ref={inputRef}
						value={draft}
					/>
					<Button disabled={!projectId || !draft.trim() || isSubmitting || pickerBusy} type="submit">
						Send
					</Button>
					{error && <p className="text-[12px] text-destructive">{error}</p>}
				</form>
			)}
		</CreateProjectFlow>
	);
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run --config vite.renderer.config.ts src/renderer/components/CodeHomeComposer.test.tsx`
Expected: PASS (3/3).

- [ ] **Step 5: Run typecheck**

Run: `cd frontend && npm run typecheck`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/renderer/components/CodeHomeComposer.tsx frontend/src/renderer/components/CodeHomeComposer.test.tsx
git commit -m "feat(ui): add CodeHomeComposer, the primary session-creation surface"
```

---

### Task 8: `CodeHome` replaces `CEODashboard` at `/`

**Files:**
- Modify: `frontend/src/renderer/routes/_shell.index.tsx`
- Create: `frontend/src/renderer/routes/_shell.index.test.tsx`

**Interfaces:**
- Consumes: `recentSessions` (Task 1), `formatRelativeTime` (Task 2), `SessionDot` (Task 5), `CodeHomeComposer` (Task 7).
- Produces: `CodeHome` as the `/` route component. `CEODashboard` (Task 3) is no longer referenced by any route after this task — it stays in the codebase, reachable only by direct import.

- [ ] **Step 1: Write the failing test**

```tsx
// frontend/src/renderer/routes/_shell.index.test.tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { WorkspaceSummary } from "../types/workspace";

const { navigateMock } = vi.hoisted(() => ({ navigateMock: vi.fn() }));

vi.mock("@tanstack/react-router", async (importOriginal) => {
	const actual = await importOriginal<typeof import("@tanstack/react-router")>();
	return { ...actual, useNavigate: () => navigateMock, createFileRoute: () => (opts: unknown) => opts };
});

vi.mock("../components/CodeHomeComposer", () => ({ CodeHomeComposer: () => <div data-testid="composer" /> }));
vi.mock("../stores/ui-store", () => ({ useUiStore: (selector: (s: { orgName: string }) => unknown) => selector({ orgName: "Vertex Holdings" }) }));

const { useWorkspaceQueryMock } = vi.hoisted(() => ({ useWorkspaceQueryMock: vi.fn() }));
vi.mock("../hooks/useWorkspaceQuery", () => ({ useWorkspaceQuery: useWorkspaceQueryMock }));

import { CodeHome } from "./_shell.index";

function renderHome(workspaces: WorkspaceSummary[]) {
	useWorkspaceQueryMock.mockReturnValue({ data: workspaces, isLoading: false });
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={queryClient}>
			<CodeHome />
		</QueryClientProvider>,
	);
}

describe("CodeHome", () => {
	it("greets by org name and always renders the composer", () => {
		renderHome([]);
		expect(screen.getByText("Welcome back, Vertex Holdings")).toBeInTheDocument();
		expect(screen.getByTestId("composer")).toBeInTheDocument();
	});

	it("shows an empty state when there are no sessions", () => {
		renderHome([]);
		expect(screen.getByText(/No sessions yet/)).toBeInTheDocument();
	});

	it("lists sessions needing attention before the rest, and navigates on click", async () => {
		const workspaces: WorkspaceSummary[] = [
			{
				id: "proj1",
				name: "Proj One",
				path: "/proj1",
				sessions: [
					{
						id: "working-sess",
						workspaceId: "proj1",
						workspaceName: "Proj One",
						title: "Working session",
						provider: "claude-code",
						branch: "main",
						status: "working",
						updatedAt: "2026-08-02T00:00:00Z",
						prs: [],
					},
					{
						id: "needs-input-sess",
						workspaceId: "proj1",
						workspaceName: "Proj One",
						title: "Needs input session",
						provider: "claude-code",
						branch: "main",
						status: "needs_input",
						updatedAt: "2026-08-01T00:00:00Z",
						prs: [],
					},
				],
			},
		];
		renderHome(workspaces);

		const titles = screen.getAllByText(/session$/).map((el) => el.textContent);
		expect(titles).toEqual(["Needs input session", "Working session"]);
	});
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run --config vite.renderer.config.ts src/renderer/routes/_shell.index.test.tsx`
Expected: FAIL — `CodeHome` is not exported from `./_shell.index` (the file still exports `CEODashboard` re-export from Task 3).

- [ ] **Step 3: Implement**

Replace the full contents of `frontend/src/renderer/routes/_shell.index.tsx`:

```tsx
// frontend/src/renderer/routes/_shell.index.tsx
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
	const needsAttention = sessions.filter((s) => attentionZone(s) === "action");
	const rest = sessions.filter((s) => attentionZone(s) !== "action");
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run --config vite.renderer.config.ts src/renderer/routes/_shell.index.test.tsx`
Expected: PASS (3/3).

- [ ] **Step 5: Run full suite + typecheck**

Run: `cd frontend && npm run test && npm run typecheck`
Expected: PASS, clean.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/renderer/routes/_shell.index.tsx frontend/src/renderer/routes/_shell.index.test.tsx
git commit -m "feat(ui): CodeHome replaces CEODashboard as Code mode's home"
```

---

### Task 9: Wire `_shell.tsx` to render `CodeSidebar`

**Files:**
- Modify: `frontend/src/renderer/routes/_shell.tsx`

**Interfaces:**
- Consumes: `CodeSidebar` (Task 6).
- Produces: Code mode's persistent shell now shows the new sidebar on every route under `_shell`, not just `/`.

- [ ] **Step 1: Swap the import**

In `frontend/src/renderer/routes/_shell.tsx`, replace:

```ts
import { Sidebar } from "../components/Sidebar";
```

with:

```ts
import { CodeSidebar } from "../components/CodeSidebar";
```

- [ ] **Step 2: Swap the render**

Replace the `<Sidebar ... />` block:

```tsx
					<Sidebar
						daemonStatus={daemonStatus}
						underTopbar
						onCreateProject={createProject}
						onRemoveProject={removeProject}
						workspaceError={workspaceQuery.isError ? errorMessage(workspaceQuery.error) : undefined}
						workspaces={workspaces}
					/>
```

with:

```tsx
					<CodeSidebar workspaces={workspaces} />
```

`removeProject` stays defined in this file (still provided via `ShellProvider` from Task 4) and `workspaceQuery`/`errorMessage` may become unused here — check with a typecheck pass in Step 4 and remove any now-dead local variable/helper the compiler flags, but do not remove `removeProject` itself (it's still exposed through context for Task 10).

- [ ] **Step 3: Run the full suite**

Run: `cd frontend && npm run test`
Expected: PASS, same count as end of Task 8 (no new tests — `_shell.tsx` has no dedicated test file; this is verified indirectly by every route test under `_shell` still passing).

- [ ] **Step 4: Run typecheck and clean up any now-unused locals**

Run: `cd frontend && npm run typecheck`
If `errorMessage` or `workspaceQuery.isError` usage becomes dead code (no longer referenced anywhere in the file), remove the now-unused helper/variable. Re-run typecheck until clean.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/renderer/routes/_shell.tsx
git commit -m "feat(ui): mount CodeSidebar as Code mode's persistent sidebar"
```

---

### Task 10: "Remove project" moves to Project Settings

**Files:**
- Modify: `frontend/src/renderer/components/ProjectSettingsForm.tsx`
- Test: `frontend/src/renderer/components/ProjectSettingsForm.test.tsx` (check if this file already exists — if so, add to it; if not, create it)

**Interfaces:**
- Consumes: `useShell().removeProject` (Task 4).
- Produces: a "Danger Zone" section at the bottom of the project settings page with a working Remove Project action — the only reachable place to remove a project now that `CodeSidebar` (Task 6/9) has no per-project row.

- [ ] **Step 1: Check for an existing test file**

Run: `ls frontend/src/renderer/components/ProjectSettingsForm.test.tsx 2>/dev/null || echo "none"`

If it exists, read it fully first so Step 2's test follows its existing render-helper/mock conventions instead of introducing a second, inconsistent pattern.

- [ ] **Step 2: Write the failing test**

If no file exists, create `frontend/src/renderer/components/ProjectSettingsForm.test.tsx` with (at minimum) this test, adapted to match the existing file's conventions if one was found in Step 1:

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

const { getMock, removeProjectMock, navigateMock } = vi.hoisted(() => ({
	getMock: vi.fn(),
	removeProjectMock: vi.fn(),
	navigateMock: vi.fn(),
}));

vi.mock("@tanstack/react-router", async (importOriginal) => {
	const actual = await importOriginal<typeof import("@tanstack/react-router")>();
	return { ...actual, useNavigate: () => navigateMock };
});

vi.mock("../lib/api-client", () => ({
	apiClient: { GET: getMock },
	apiErrorMessage: (error: unknown, fallback = "Request failed") => (error instanceof Error ? error.message : fallback),
}));

vi.mock("../lib/shell-context", () => ({ useShell: () => ({ removeProject: removeProjectMock }) }));
vi.mock("../hooks/useWorkspaceQuery", () => ({ useWorkspaceQuery: () => ({ data: [] }) }));

import { ProjectSettingsForm } from "./ProjectSettingsForm";

describe("ProjectSettingsForm danger zone", () => {
	it("removes the project and navigates home after confirmation", async () => {
		const user = userEvent.setup();
		getMock.mockResolvedValue({
			data: { status: "ok", project: { id: "proj1", path: "/proj1", config: {} } },
			error: undefined,
		});
		removeProjectMock.mockResolvedValue(undefined);
		vi.spyOn(window, "confirm").mockReturnValue(true);

		const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
		render(
			<QueryClientProvider client={queryClient}>
				<ProjectSettingsForm projectId="proj1" />
			</QueryClientProvider>,
		);

		await user.click(await screen.findByRole("button", { name: /remove project/i }));

		await waitFor(() => expect(removeProjectMock).toHaveBeenCalledWith("proj1"));
		await waitFor(() => expect(navigateMock).toHaveBeenCalledWith({ to: "/" }));
	});
});
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd frontend && npx vitest run --config vite.renderer.config.ts src/renderer/components/ProjectSettingsForm.test.tsx`
Expected: FAIL — no "Remove project" button exists yet.

- [ ] **Step 4: Implement the Danger Zone section**

In `frontend/src/renderer/components/ProjectSettingsForm.tsx`:

1. Add imports: `useNavigate` from `"@tanstack/react-router"`, `useShell` from `"../lib/shell-context"`.
2. In `SettingsBody`, add state and a handler:

```tsx
	const navigate = useNavigate();
	const { removeProject } = useShell();
	const [isRemoving, setIsRemoving] = useState(false);
	const [removeError, setRemoveError] = useState<string | null>(null);

	const handleRemoveProject = async () => {
		const confirmed = window.confirm(
			`Remove project ${project.path}? This stops its live sessions and removes it from the app, but keeps the repository folder and stored history on disk.`,
		);
		if (!confirmed) return;
		setRemoveError(null);
		setIsRemoving(true);
		try {
			await removeProject(projectId);
			void navigate({ to: "/" });
		} catch (err) {
			setRemoveError(err instanceof Error ? err.message : "Could not remove project");
		} finally {
			setIsRemoving(false);
		}
	};
```

3. At the end of `SettingsBody`'s returned JSX (after the existing settings sections, still inside the outermost wrapping element), add:

```tsx
			<Card className="border-destructive/30">
				<CardHeader>
					<CardTitle className="text-destructive">Danger Zone</CardTitle>
				</CardHeader>
				<CardContent className="space-y-3">
					<p className="text-[13px] text-passive">
						Stops this project's live sessions and removes it from the app. The repository folder and stored history
						stay on disk.
					</p>
					<Button disabled={isRemoving} onClick={() => void handleRemoveProject()} variant="destructive">
						{isRemoving ? "Removing…" : "Remove project"}
					</Button>
					{removeError && <p className="text-[12px] text-destructive">{removeError}</p>}
				</CardContent>
			</Card>
```

(Check `./ui/button`'s `Button` component supports a `variant="destructive"` — `Sidebar.tsx`'s existing "Remove project" menu item uses `className="text-destructive ..."` on a `DropdownMenuItem`, not a `Button` variant; if `Button` has no `destructive` variant, use `variant="outline"` with an added `className="border-destructive text-destructive hover:bg-destructive/10"` instead — check `frontend/src/renderer/components/ui/button.tsx`'s variant list before writing this line and use whatever the file actually supports.)

- [ ] **Step 5: Run test to verify it passes**

Run: `cd frontend && npx vitest run --config vite.renderer.config.ts src/renderer/components/ProjectSettingsForm.test.tsx`
Expected: PASS.

- [ ] **Step 6: Run full suite + typecheck**

Run: `cd frontend && npm run test && npm run typecheck`
Expected: PASS, clean.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/renderer/components/ProjectSettingsForm.tsx frontend/src/renderer/components/ProjectSettingsForm.test.tsx
git commit -m "feat(ui): move Remove project into the settings page's Danger Zone"
```

---

### Task 11: End-to-end smoke in the running app

**Files:**
- None (verification only). Fix regressions in files touched by Tasks 1-10 if found.

- [ ] **Step 1: Launch the app**

Run: `cd frontend && VITE_NO_ELECTRON=1 npx vite --config vite.renderer.config.ts --port 5183` (web preview — uses mock workspace data; real daemon flows can't be exercised this way, see Step 3).

- [ ] **Step 2: Verify by hand (mock-data checks)**

- `/` shows "Welcome back, {org}", a Sessions list (attention-needing sessions first) or the empty state, and the composer pinned at the bottom.
- `CodeSidebar` shows New / Recents / More; Recents lists sessions from the mock workspaces; clicking one navigates to it.
- Clicking New (when already on `/`) focuses the composer's prompt input.
- More opens and lists Pull requests / Live Terminals / theme toggle / Settings; each navigates correctly.
- The composer's project picker lists the mock projects; "New project…" opens the existing path-picker dialog (same as before this plan).

- [ ] **Step 3: Note what mock mode can't verify**

Web preview mode short-circuits `useWorkspaceQuery`/`createProject` to mock data (`VITE_NO_ELECTRON=1`), so the actual spawn → send-first-message → navigate chain against a real daemon is not exercised by this step. That path is covered by Task 7's unit test (mocked at the API-client boundary, asserting the exact call sequence) — note in your report that real-daemon verification is unit-test-only for this plan, consistent with how the mode-shell plan's own Task 5 handled the same limitation.

- [ ] **Step 4: Demo**

From inside a real session (not the ad hoc web preview), run `ao preview` so the change renders in the desktop browser panel per CLAUDE.md.

- [ ] **Step 5: Final full check**

Run: `cd frontend && npm run test && npm run typecheck`
Expected: PASS, clean.

```bash
git add -A frontend/src
git commit -m "fix(ui): moden-code-home smoke fixes"   # only if fixes were needed
```

---

## Out of scope (explicitly)

- Company/HQ concept removal (companies, CEO Dashboard's routing, "Watch Live", HQ roles) — tracked as its own future spec per the design doc.
- Artifacts / Customize nav items from the reference screenshot — no equivalent concept in moden-agent.
- Code Manage mode and Work mode — untouched.
- Backend/daemon changes — none.
- Multi-line prompt input / rich composer — the composer uses the same single-line `Input` + Enter-to-submit pattern as `TerminalTile`'s existing compose bar, not a new textarea primitive.
