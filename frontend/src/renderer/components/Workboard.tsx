import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, SlidersHorizontal } from "lucide-react";
import { type DragEvent, useEffect, useMemo, useState } from "react";
import type { components } from "../../api/schema";
import { useWorkboardCards, workboardQueryKey, type WorkCard as WorkboardCard } from "../hooks/useWorkboardQuery";
import { apiClient, apiErrorMessage } from "../lib/api-client";
import { cn } from "../lib/utils";
import { CreateWorkCardDialog } from "./CreateWorkCardDialog";
import { DashboardSubhead } from "./DashboardSubhead";
import { WorkCard } from "./WorkCard";
import { WorkCardFocusPanel } from "./WorkCardFocusPanel";
import { Button } from "./ui/button";
import { useWorkspaceQuery } from "../hooks/useWorkspaceQuery";
import { useShell } from "../lib/shell-context";
import { useUiStore } from "../stores/ui-store";
import { Sheet, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle } from "./ui/sheet";
import { Label } from "./ui/label";

type CardStatus = WorkboardCard["status"];
type MoveWorkCardRequest = components["schemas"]["MoveWorkCardRequest"];
type Project = components["schemas"]["Project"];
type AutonomousConfig = components["schemas"]["WorkboardAutonomousConfig"];

const projectQueryKey = (id: string) => ["project", id] as const;
const AUTONOMOUS_MODE_OPTIONS = [
	{ value: "skip_timeout", label: "Skip timeout" },
	{ value: "short_timeout", label: "Short timeout" },
] as const;

export const WORKBOARD_COLUMNS: { status: CardStatus; label: string; rail: string }[] = [
	{ status: "triage", label: "Triage", rail: "var(--purple)" },
	{ status: "backlog", label: "Backlog", rail: "var(--fg-passive)" },
	{ status: "todo", label: "To do", rail: "var(--accent)" },
	{ status: "scheduled", label: "Scheduled", rail: "var(--amber)" },
	{ status: "ready", label: "Ready", rail: "var(--green)" },
	{ status: "running", label: "Running", rail: "var(--orange)" },
	{ status: "review", label: "Review", rail: "var(--accent)" },
	{ status: "blocked", label: "Blocked", rail: "var(--red)" },
	{ status: "done", label: "Done", rail: "var(--fg-passive)" },
];

export function Workboard({ projectId, onShowSessions }: { projectId: string; onShowSessions?: () => void }) {
	const queryClient = useQueryClient();
	const cardsQuery = useWorkboardCards(projectId);
	const workspaceQuery = useWorkspaceQuery();
	const { daemonStatus } = useShell();
	const theme = useUiStore((state) => state.theme);
	const [focusedCardId, setFocusedCardId] = useState<string>();
	const [isCreateOpen, setIsCreateOpen] = useState(false);
	const [draggedCardId, setDraggedCardId] = useState<string>();
	const [moveError, setMoveError] = useState<string>();
	const [moveAnnouncement, setMoveAnnouncement] = useState<string>();
	const [isAutonomousOpen, setIsAutonomousOpen] = useState(false);
	const cardsByStatus = useMemo(() => {
		const grouped = new Map<CardStatus, WorkboardCard[]>();
		for (const card of cardsQuery.data ?? []) (grouped.get(card.status) ?? grouped.set(card.status, []).get(card.status)!).push(card);
		for (const cards of grouped.values()) cards.sort((a, b) => a.position - b.position);
		return grouped;
	}, [cardsQuery.data]);
	const moveCard = useMutation({
		mutationFn: async ({ cardId, status, position }: { cardId: string; status: CardStatus; position: number }) => {
			const body: MoveWorkCardRequest = { status, position };
			const { error } = await apiClient.POST("/api/v1/workboard/cards/{cardId}/move", { params: { path: { cardId } }, body });
			if (error) throw new Error(apiErrorMessage(error, "Could not move work card."));
		},
		onSuccess: () => queryClient.invalidateQueries({ queryKey: workboardQueryKey(projectId) }),
		onError: (error) => setMoveError(error instanceof Error ? error.message : "Could not move work card."),
	});
	const moveCardTo = (cardId: string, status: CardStatus, position: number) => {
		if (moveCard.isPending) return;
		setMoveError(undefined);
		const destination = WORKBOARD_COLUMNS.find((column) => column.status === status)?.label ?? status;
		moveCard.mutate(
			{ cardId, status, position },
			{ onSuccess: () => setMoveAnnouncement(`Moved card to ${destination}.`) },
		);
	};
	const handleDrop = (event: DragEvent<HTMLElement>, status: CardStatus, position: number) => {
		event.preventDefault();
		const cardId = event.dataTransfer.getData("text/plain") || draggedCardId;
		setDraggedCardId(undefined);
		if (!cardId) return;
		moveCardTo(cardId, status, position);
	};
	const moveFocusedCard = (card: WorkboardCard, direction: "previous" | "next") => {
		const columnIndex = WORKBOARD_COLUMNS.findIndex((column) => column.status === card.status);
		const destination = WORKBOARD_COLUMNS[columnIndex + (direction === "previous" ? -1 : 1)];
		if (!destination) return;
		moveCardTo(card.id, destination.status, (cardsByStatus.get(destination.status) ?? []).length);
	};
	const focusedCard = (cardsQuery.data ?? []).find((card) => card.id === focusedCardId);
	const focusedSession = focusedCard?.sessionId
		? workspaceQuery.data?.flatMap((workspace) => workspace.sessions).find((session) => session.id === focusedCard.sessionId)
		: undefined;

	return (
		<div className="flex h-full min-h-0 flex-col bg-background text-foreground">
			<DashboardSubhead
				title="Workboard"
				subtitle="Durable work cards in OpenClaw flow order."
				actions={<><Button onClick={() => setIsAutonomousOpen(true)} size="sm" variant="ghost"><SlidersHorizontal className="size-3.5" aria-hidden="true" />Autonomous</Button><Button onClick={() => setIsCreateOpen(true)} size="sm"><Plus className="size-3.5" aria-hidden="true" />Create card</Button>{onShowSessions ? <Button onClick={onShowSessions} size="sm" variant="ghost">Sessions</Button> : null}</>}
			/>
			<p className="sr-only" id="workboard-keyboard-help">Press Left or Right Arrow to move the focused card between columns.</p>
			<div className="flex min-h-0 flex-1">
				<div className="min-w-0 flex-1 overflow-x-auto overflow-y-hidden p-[18px]">
					{cardsQuery.isError ? <p className="py-10 text-center text-[12px] text-passive">Could not load workboard.</p> : (
						<div className="grid h-full min-w-[1540px] grid-cols-9 gap-2">
							{WORKBOARD_COLUMNS.map((column) => {
								const cards = cardsByStatus.get(column.status) ?? [];
								return <section aria-label={`${column.label} column, ${cards.length} cards`} className={cn("relative flex min-w-0 flex-col overflow-hidden rounded-[10px] bg-[var(--kanban-column-bg)]")} key={column.status} onDragOver={(event) => event.preventDefault()} onDrop={(event) => handleDrop(event, column.status, cards.length)}>
									<div className="absolute inset-y-0 left-0 w-[2px]" style={{ background: column.rail }} />
									<div className="flex shrink-0 items-center gap-2 px-3 pb-2.5 pt-3">
										<span className="text-[10.5px] font-semibold uppercase tracking-[0.08em] text-muted-foreground">{column.label}</span>
										<span className="ml-auto font-mono text-[10px] text-passive">{cards.length}</span>
									</div>
									<div className="min-h-0 flex-1 overflow-y-auto px-2 pb-3">
										<div className="flex flex-col gap-2">
											{cards.map((card) => <WorkCard card={card} key={card.id} onDragStart={setDraggedCardId} onMove={(direction) => moveFocusedCard(card, direction)} onFocus={(focused) => setFocusedCardId(focused.id)} selected={card.id === focusedCardId} />)}
										</div>
									</div>
								</section>;
							})}
						</div>
					)}
				</div>
				{focusedCard ? <WorkCardFocusPanel card={focusedCard} session={focusedSession} theme={theme} daemonReady={daemonStatus.state === "ready"} onClose={() => setFocusedCardId(undefined)} onShowSessions={onShowSessions} /> : null}
			</div>
			{moveError ? <p className="px-[18px] pb-3 text-[12px] text-destructive" role="alert">{moveError}</p> : null}
			{moveAnnouncement ? <p aria-live="polite" className="sr-only">{moveAnnouncement}</p> : null}
			<CreateWorkCardDialog open={isCreateOpen} projectId={projectId} onCreated={() => undefined} onOpenChange={setIsCreateOpen} />
			<AutonomousSettings projectId={projectId} open={isAutonomousOpen} onOpenChange={setIsAutonomousOpen} />
		</div>
	);
}

function AutonomousSettings({ projectId, open, onOpenChange }: { projectId: string; open: boolean; onOpenChange: (open: boolean) => void }) {
	const queryClient = useQueryClient();
	const projectQuery = useQuery({
		queryKey: projectQueryKey(projectId),
		enabled: open,
		queryFn: async () => {
			const { data, error } = await apiClient.GET("/api/v1/projects/{id}", { params: { path: { id: projectId } } });
			if (error || data?.status !== "ok") throw new Error(apiErrorMessage(error, "Could not load autonomous settings."));
			return data.project as Project;
		},
	});
	const loaded = projectQuery.data?.config?.workboard?.autonomous;
	const defaults: Required<AutonomousConfig> = { enabled: false, mode: "skip_timeout", shortTimeoutMinutes: 2, sticky: true };
	const [form, setForm] = useState(defaults);
	const [loadedProjectId, setLoadedProjectId] = useState<string>();
	useEffect(() => {
		if (projectQuery.data && loadedProjectId !== projectQuery.data.id) {
			setLoadedProjectId(projectQuery.data.id);
			setForm({ ...defaults, ...loaded });
		}
	}, [loaded, loadedProjectId, projectQuery.data]);
	const mutation = useMutation({
		mutationFn: async () => {
			if (!projectQuery.data) throw new Error("Project config is unavailable.");
			const nextConfig = {
				...projectQuery.data.config,
				workboard: {
					...projectQuery.data.config?.workboard,
					autonomous: { ...form, shortTimeoutMinutes: Math.max(1, Math.round(form.shortTimeoutMinutes)) },
				},
			};
			const { error } = await apiClient.PUT("/api/v1/projects/{id}/config", { params: { path: { id: projectId } }, body: { config: nextConfig } });
			if (error) throw new Error(apiErrorMessage(error, "Could not save autonomous settings."));
		},
		onSuccess: () => {
			void queryClient.invalidateQueries({ queryKey: projectQueryKey(projectId) });
			onOpenChange(false);
		},
	});

	return <Sheet open={open} onOpenChange={onOpenChange}>
		<SheetContent aria-label="Autonomous settings" className="border-slate-700 bg-background sm:max-w-md">
			<SheetHeader className="border-b border-border px-5 py-4">
				<SheetTitle className="text-base">Autonomous mode</SheetTitle>
				<SheetDescription>Let Hermes answer eligible workboard prompts on your behalf.</SheetDescription>
			</SheetHeader>
			<div className="flex flex-col gap-5 overflow-y-auto px-5 py-4">
				{projectQuery.isLoading ? <p className="text-[12px] text-muted-foreground">Loading settings…</p> : null}
				{projectQuery.isError ? <p className="text-[12px] text-error" role="alert">{projectQuery.error instanceof Error ? projectQuery.error.message : "Could not load settings."}</p> : null}
				{projectQuery.data ? <>
					<label className="flex items-start gap-3 rounded-md border border-border bg-card p-3">
						<input aria-label="Enable autonomous mode" checked={form.enabled} className="mt-0.5 accent-[var(--accent)]" type="checkbox" onChange={(event) => setForm((current) => ({ ...current, enabled: event.target.checked }))} />
						<span><span className="block text-[13px] font-medium">Enable autonomous mode</span><span className="mt-1 block text-[12px] text-muted-foreground">Automatically handle allowed questions after the configured timeout.</span></span>
					</label>
					<div className="flex flex-col gap-1.5"><Label htmlFor="autonomous-mode" className="text-[12px] text-muted-foreground">Mode</Label><select id="autonomous-mode" value={form.mode} className="h-8 rounded-md border border-input bg-transparent px-2.5 text-[13px] text-foreground" onChange={(event) => setForm((current) => ({ ...current, mode: event.target.value }))}>{AUTONOMOUS_MODE_OPTIONS.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}</select></div>
					<div className="flex flex-col gap-1.5"><Label htmlFor="autonomous-short-minutes" className="text-[12px] text-muted-foreground">Short timeout (minutes)</Label><input id="autonomous-short-minutes" min={1} step={1} type="number" value={form.shortTimeoutMinutes} className="h-8 rounded-md border border-input bg-transparent px-2.5 text-[13px] text-foreground" onChange={(event) => setForm((current) => ({ ...current, shortTimeoutMinutes: Number(event.target.value) || 1 }))} /></div>
					<label className="flex items-center gap-3 text-[13px]"><input aria-label="Keep autonomous mode enabled" checked={form.sticky} className="accent-[var(--accent)]" type="checkbox" onChange={(event) => setForm((current) => ({ ...current, sticky: event.target.checked }))} /><span>Keep enabled for future prompts</span></label>
				</> : null}
			</div>
			<SheetFooter className="border-t border-border px-5 py-4"><Button disabled={!projectQuery.data || mutation.isPending} onClick={() => mutation.mutate()}>{mutation.isPending ? "Saving…" : "Save changes"}</Button>{mutation.isError ? <p className="text-[12px] text-error" role="alert">{mutation.error instanceof Error ? mutation.error.message : "Could not save settings."}</p> : null}</SheetFooter>
		</SheetContent>
	</Sheet>;
}
