import { useMutation, useQueryClient } from "@tanstack/react-query";
import { MoreHorizontal, Split, Target, Trash2, Zap } from "lucide-react";
import { type FormEvent, useEffect, useId, useState } from "react";
import type { components } from "../../api/schema";
import {
	useDispatchProject,
	useWorkCardDispatchFailure,
	useWorkCardRedo,
	workboardQueryKey,
	type WorkCard as WorkboardCard,
} from "../hooks/useWorkboardQuery";
import { apiClient, apiErrorMessage } from "../lib/api-client";
import { formatDatetimeLocalValue, formatScheduledAtDisplay, parseDatetimeLocalValue } from "../lib/workboard-schedule";
import type { Theme } from "../stores/ui-store";
import type { WorkspaceSession } from "../types/workspace";
import { RedoCyclePanel } from "./RedoCyclePanel";
import { TerminalPane } from "./TerminalPane";
import { Button } from "./ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "./ui/dropdown-menu";
import { Input } from "./ui/input";
import { Label } from "./ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./ui/select";
import { Sheet, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle } from "./ui/sheet";

const TERMINAL_FONT_SIZE = 12;

type NudgeWorkCardRequest = components["schemas"]["NudgeWorkCardRequest"];
type RetargetWorkCardRequest = components["schemas"]["RetargetWorkCardRequest"];
type SplitWorkCardRequest = components["schemas"]["SplitWorkCardRequest"];
type SplitWorkCardResponse = components["schemas"]["SplitWorkCardResponse"];
type UpdateWorkCardRequest = components["schemas"]["UpdateWorkCardRequest"];
type SplitFate = NonNullable<SplitWorkCardRequest["oldCardFate"]>;

export function WorkCardFocusPanel({
	card,
	projectId,
	session,
	theme,
	daemonReady,
	onClose,
	onShowSessions,
}: {
	card: WorkboardCard;
	projectId: string;
	session?: WorkspaceSession;
	theme: Theme;
	daemonReady: boolean;
	onClose: () => void;
	onShowSessions?: () => void;
}) {
	const queryClient = useQueryClient();
	const [actionError, setActionError] = useState<string>();
	const [nudgeOpen, setNudgeOpen] = useState(false);
	const [retargetOpen, setRetargetOpen] = useState(false);
	const [splitOpen, setSplitOpen] = useState(false);
	const [deleteConfirming, setDeleteConfirming] = useState(false);
	const [scheduleError, setScheduleError] = useState<string>();
	const [scheduledAtLocal, setScheduledAtLocal] = useState("");
	const [livePreviewOpen, setLivePreviewOpen] = useState(false);
	const showTerminal = livePreviewOpen && card.status === "running" && Boolean(card.sessionId && session);
	const isRunning = card.status === "running";
	const isScheduled = card.status === "scheduled";
	const isHermesCommander = session?.kind === "orchestrator" && session.harness === "hermes";

	useEffect(() => {
		setScheduledAtLocal(card.scheduledAt ? formatDatetimeLocalValue(new Date(card.scheduledAt)) : "");
		setScheduleError(undefined);
	}, [card.id, card.scheduledAt]);

	useEffect(() => {
		setLivePreviewOpen(false);
		setDeleteConfirming(false);
	}, [card.id]);

	const invalidate = async () => {
		await queryClient.invalidateQueries({ queryKey: workboardQueryKey(projectId) });
	};

	const nudge = useMutation({
		mutationFn: async (body: NudgeWorkCardRequest) => {
			const { error } = await apiClient.POST("/api/v1/workboard/cards/{cardId}/nudge", {
				params: { path: { cardId: card.id } },
				body,
			});
			if (error) throw new Error(apiErrorMessage(error, "Could not nudge work card."));
		},
		onSuccess: async () => {
			setActionError(undefined);
			setNudgeOpen(false);
			await invalidate();
		},
		onError: (error) => setActionError(error instanceof Error ? error.message : "Could not nudge work card."),
	});

	const retarget = useMutation({
		mutationFn: async (body: RetargetWorkCardRequest) => {
			const { error } = await apiClient.POST("/api/v1/workboard/cards/{cardId}/retarget", {
				params: { path: { cardId: card.id } },
				body,
			});
			if (error) throw new Error(apiErrorMessage(error, "Could not retarget work card."));
		},
		onSuccess: async () => {
			setActionError(undefined);
			setRetargetOpen(false);
			await invalidate();
		},
		onError: (error) => setActionError(error instanceof Error ? error.message : "Could not retarget work card."),
	});

	const split = useMutation({
		mutationFn: async (body: SplitWorkCardRequest) => {
			const { data, error } = await apiClient.POST("/api/v1/workboard/cards/{cardId}/split", {
				params: { path: { cardId: card.id } },
				body,
			});
			if (error) throw new Error(apiErrorMessage(error, "Could not split work card."));
			return data as SplitWorkCardResponse;
		},
		onSuccess: async () => {
			setActionError(undefined);
			setSplitOpen(false);
			await invalidate();
		},
		onError: (error) => setActionError(error instanceof Error ? error.message : "Could not split work card."),
	});

	const updateSchedule = useMutation({
		mutationFn: async (body: UpdateWorkCardRequest) => {
			const { error } = await apiClient.PATCH("/api/v1/workboard/cards/{cardId}", {
				params: { path: { cardId: card.id } },
				body,
			});
			if (error) throw new Error(apiErrorMessage(error, "Could not update schedule."));
		},
		onSuccess: async () => {
			setScheduleError(undefined);
			await invalidate();
		},
		onError: (error) => setScheduleError(error instanceof Error ? error.message : "Could not update schedule."),
	});

	const remove = useMutation({
		mutationFn: async () => {
			const { error } = await apiClient.DELETE("/api/v1/workboard/cards/{cardId}", {
				params: { path: { cardId: card.id } },
			});
			if (error) throw new Error(apiErrorMessage(error, "Could not delete work card."));
		},
		onSuccess: async () => {
			setActionError(undefined);
			setDeleteConfirming(false);
			await invalidate();
			onClose();
		},
		onError: (error) => setActionError(error instanceof Error ? error.message : "Could not delete work card."),
	});

	const saveSchedule = (event: FormEvent<HTMLFormElement>) => {
		event.preventDefault();
		if (updateSchedule.isPending) return;
		const scheduledAt = parseDatetimeLocalValue(scheduledAtLocal);
		if (!scheduledAt) {
			setScheduleError("Choose a valid schedule time.");
			return;
		}
		updateSchedule.mutate({ scheduledAt });
	};

	const redoQuery = useWorkCardRedo(card.id, (card.redoCount ?? 0) > 0 || card.status === "redo");
	const failureQuery = useWorkCardDispatchFailure(card.id, card.status === "todo");
	const dispatchMutation = useDispatchProject(projectId);

	return (
		<aside aria-label={`Focus panel for ${card.title}`} className="flex h-full w-[400px] shrink-0 flex-col border-l border-border bg-surface">
			<div className="flex shrink-0 items-start gap-3 border-b border-border px-4 py-3">
				<div className="min-w-0 flex-1">
					<div className="font-mono text-[10px] font-semibold uppercase tracking-[0.08em] text-accent">Focused card</div>
					<h2 className="mt-1 line-clamp-2 text-[14px] font-medium leading-[1.35] text-foreground">{card.title}</h2>
				</div>
				<DropdownMenu>
						<DropdownMenuTrigger asChild>
							<Button aria-label="Card actions" size="icon-sm" variant="ghost">
								<MoreHorizontal className="size-4" aria-hidden="true" />
							</Button>
						</DropdownMenuTrigger>
						<DropdownMenuContent align="end" className="min-w-44">
							{isRunning ? <>
							<DropdownMenuItem onSelect={() => setNudgeOpen(true)}>
								<Zap className="size-3.5" aria-hidden="true" />
								{isHermesCommander ? "Nudge commander" : "Nudge agent"}
							</DropdownMenuItem>
							<DropdownMenuItem onSelect={() => setRetargetOpen(true)}>
								<Target className="size-3.5" aria-hidden="true" />
								Retarget goal
							</DropdownMenuItem>
							<DropdownMenuItem onSelect={() => setSplitOpen(true)}>
								<Split className="size-3.5" aria-hidden="true" />
								Split card
							</DropdownMenuItem>
							</> : null}
							<DropdownMenuItem className="text-destructive focus:text-destructive" onSelect={() => setDeleteConfirming(true)}>
								<Trash2 className="size-3.5 text-destructive" aria-hidden="true" />
								Delete card
							</DropdownMenuItem>
						</DropdownMenuContent>
					</DropdownMenu>
				<Button aria-label="Close card focus panel" onClick={onClose} size="icon-sm" variant="ghost">×</Button>
			</div>
			<div className="min-h-0 flex-1 overflow-y-auto px-4 py-4">
				<div className="space-y-4">
					<section className="rounded-md border border-border bg-raised/30 p-3">
						<div className="flex items-center justify-between gap-3 text-[11px]">
							<span className="text-muted-foreground">Status</span>
							<span className="rounded bg-accent/10 px-2 py-1 font-mono text-[10px] font-semibold uppercase tracking-[0.06em] text-accent">{card.status}</span>
						</div>
						{card.sessionId ? (
							<div className="mt-3 border-t border-border pt-3">
								<p className="font-mono text-[10px] uppercase tracking-[0.08em] text-passive">{isHermesCommander ? "Work owner" : "Linked session"}</p>
								<p className="mt-1 text-[12px] leading-[1.45] text-foreground">
									{session ? (isHermesCommander ? <>Hermes coordinates this task <span className="text-muted-foreground">· {card.sessionId}</span></> : <>Session {card.sessionId}</>) : "The linked session is unavailable."}
								</p>
								{isHermesCommander ? <p className="mt-1 text-[11px] leading-[1.45] text-muted-foreground">Hermes reads the brief, plans the work, and assigns the coding worker.</p> : null}
							</div>
						) : <p className="mt-3 border-t border-border pt-3 text-[11px] leading-[1.45] text-passive">No session is linked to this card yet.</p>}
					</section>

					{failureQuery.data ? (
						<section className="rounded-md border border-destructive/40 bg-destructive/5 p-3" aria-label="Dispatch failure">
							<div className="flex items-start justify-between gap-2">
								<div>
									<p className="font-mono text-[10px] font-semibold uppercase tracking-[0.08em] text-destructive">Failed to start</p>
									<p className="mt-1 text-[11px] leading-[1.45] text-muted-foreground">
										{failureQuery.data.reason === "hermes_unavailable"
											? "Hermes commander unavailable"
											: failureQuery.data.reason === "non_hermes_orchestrator"
											? "A non-Hermes orchestrator is active"
											: "Worker spawn failed"}
									</p>
									<p className="mt-0.5 text-[10px] text-passive">
										Attempted {new Date(failureQuery.data.attemptedAt).toLocaleTimeString()}
									</p>
								</div>
								<Button
									className="shrink-0"
									disabled={dispatchMutation.isPending || !daemonReady}
									onClick={() => dispatchMutation.mutate()}
									size="sm"
									title={daemonReady ? "Retry dispatch for this project" : "Daemon offline — reconnect to retry"}
									variant="outline"
								>
									{dispatchMutation.isPending ? "Retrying…" : "Retry dispatch"}
								</Button>
							</div>
							{!daemonReady ? (
								<p className="mt-2 text-[10px] text-destructive">Daemon offline — reconnect Modern Agent to resume.</p>
							) : null}
						</section>
					) : null}

					<section className="space-y-2">
						<p className="font-mono text-[10px] font-semibold uppercase tracking-[0.08em] text-accent">Task details</p>
						<p className="whitespace-pre-wrap break-words text-[12px] leading-[1.6] text-muted-foreground">{card.notes?.trim() || "No additional instructions were provided."}</p>
					</section>

					<section className="grid grid-cols-2 gap-x-4 gap-y-3 border-y border-border py-3 text-[11px]">
						<Detail label="Coding worker" value={card.codingAgent || card.agent || "Automatic"} />
						<Detail label="Goal version" value={`v${card.goalVersion ?? 1}`} />
						<Detail label="Project path" value={card.targetPath} className="col-span-2" />
						{(card.labels ?? []).length > 0 ? <Detail label="Labels" value={card.labels.join(" · ")} className="col-span-2" /> : null}
					</section>
					{(card.redoCount ?? 0) > 0 || card.status === "redo" ? (
						<RedoCyclePanel cycles={redoQuery.data ?? []} latestSummary={card.latestRedoSummary} />
					) : null}
					{isScheduled ? (
						<form className="space-y-2" onSubmit={saveSchedule}>
							<Label className="text-[11px] text-muted-foreground" htmlFor={`schedule-${card.id}`}>Scheduled for</Label>
							<Input id={`schedule-${card.id}`} onChange={(event) => setScheduledAtLocal(event.target.value)} type="datetime-local" value={scheduledAtLocal} />
							{card.scheduledAt ? <p className="text-[10px] text-passive">Currently {formatScheduledAtDisplay(card.scheduledAt)}</p> : null}
							{scheduleError ? <p className="text-[11px] text-destructive" role="alert">{scheduleError}</p> : null}
							<Button disabled={updateSchedule.isPending || !scheduledAtLocal.trim()} size="sm" type="submit" variant="outline">
								{updateSchedule.isPending ? "Saving..." : "Save schedule"}
							</Button>
						</form>
					) : card.scheduledAt ? (
						<div className="text-[11px] text-muted-foreground">Scheduled for {formatScheduledAtDisplay(card.scheduledAt)}</div>
					) : null}
					{card.sessionId && session ? (
						<div className="flex flex-wrap gap-2 pt-1">
							{isRunning ? <Button onClick={() => setLivePreviewOpen((open) => !open)} size="sm" variant="outline">{showTerminal ? "Hide live terminal" : "Show live terminal"}</Button> : null}
							{onShowSessions ? <Button onClick={onShowSessions} size="sm" variant="ghost">View all sessions</Button> : null}
						</div>
					) : null}
					{deleteConfirming ? (
						<section className="space-y-3 rounded-md border border-destructive/50 bg-destructive/5 p-3" role="alert">
							<div>
								<p className="text-[12px] font-medium text-foreground">Delete this card permanently?</p>
								<p className="mt-1 text-[11px] leading-[1.45] text-muted-foreground">The card and its history will be removed. {card.sessionId ? "Its linked session will keep running; stop it separately from Sessions." : ""}</p>
							</div>
							<div className="flex gap-2">
								<Button className="border-destructive bg-destructive text-destructive-foreground hover:opacity-90" disabled={remove.isPending} onClick={() => remove.mutate()} size="sm">{remove.isPending ? "Deleting..." : "Confirm delete card"}</Button>
								<Button disabled={remove.isPending} onClick={() => setDeleteConfirming(false)} size="sm" variant="ghost">Cancel</Button>
							</div>
						</section>
					) : null}
					{actionError ? <p className="text-[11px] text-destructive" role="alert">{actionError}</p> : null}
				</div>
			</div>
			{showTerminal ? (
				<div className="border-t border-border p-4">
					<div className="mb-2 flex items-center justify-between gap-3">
						<p className="font-mono text-[10px] font-semibold uppercase tracking-[0.08em] text-accent">Live terminal</p>
						<p className="text-[10px] text-passive">Interactive session output</p>
					</div>
					<div className="h-[300px] overflow-hidden rounded-md border border-border">
						<TerminalPane session={session} theme={theme} daemonReady={daemonReady} fontSize={TERMINAL_FONT_SIZE} />
					</div>
				</div>
			) : null}
			<NudgeSheet
				open={nudgeOpen}
				onOpenChange={setNudgeOpen}
				pending={nudge.isPending}
				commander={isHermesCommander}
				onSubmit={(message) => nudge.mutate({ message })}
			/>
			<RetargetSheet
				key={card.id}
				card={card}
				open={retargetOpen}
				onOpenChange={setRetargetOpen}
				pending={retarget.isPending}
				onSubmit={(body) => retarget.mutate(body)}
			/>
			<SplitSheet
				key={card.id}
				card={card}
				open={splitOpen}
				onOpenChange={setSplitOpen}
				pending={split.isPending}
				onSubmit={(body) => split.mutate(body)}
			/>
		</aside>
	);
}

function Detail({ label, value, className }: { label: string; value: string; className?: string }) {
	return (
		<div className={className}>
			<p className="font-mono text-[9.5px] uppercase tracking-[0.07em] text-passive">{label}</p>
			<p className="mt-1 break-words text-[11px] leading-[1.4] text-foreground">{value}</p>
		</div>
	);
}

function NudgeSheet({
	open,
	onOpenChange,
	pending,
	commander,
	onSubmit,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	pending: boolean;
	commander: boolean;
	onSubmit: (message: string) => void;
}) {
	const messageId = useId();
	const [message, setMessage] = useState("");
	const submit = (event: FormEvent<HTMLFormElement>) => {
		event.preventDefault();
		const clean = message.trim();
		if (!clean || pending) return;
		onSubmit(clean);
		setMessage("");
	};
	return (
		<Sheet onOpenChange={onOpenChange} open={open}>
			<SheetContent className="sm:max-w-md">
				<form onSubmit={submit}>
					<SheetHeader>
						<SheetTitle>{commander ? "Nudge commander" : "Nudge agent"}</SheetTitle>
					<SheetDescription>{commander ? "Send an instruction to Hermes without changing the card column." : "Send a message to the linked coding session without changing the card column."}</SheetDescription>
					</SheetHeader>
					<div className="px-4 py-4">
						<Label htmlFor={messageId}>Message</Label>
						<Input id={messageId} onChange={(event) => setMessage(event.target.value)} value={message} />
					</div>
					<SheetFooter>
						<Button disabled={pending || !message.trim()} type="submit">Send nudge</Button>
					</SheetFooter>
				</form>
			</SheetContent>
		</Sheet>
	);
}

function RetargetSheet({
	card,
	open,
	onOpenChange,
	pending,
	onSubmit,
}: {
	card: WorkboardCard;
	open: boolean;
	onOpenChange: (open: boolean) => void;
	pending: boolean;
	onSubmit: (body: RetargetWorkCardRequest) => void;
}) {
	const titleId = useId();
	const notesId = useId();
	const [title, setTitle] = useState(card.title);
	const [notes, setNotes] = useState(card.notes);
	const submit = (event: FormEvent<HTMLFormElement>) => {
		event.preventDefault();
		if (pending) return;
		const body: RetargetWorkCardRequest = {};
		if (title.trim() !== card.title) body.title = title.trim();
		if (notes.trim() !== card.notes) body.notes = notes.trim();
		if (!body.title && !body.notes) return;
		onSubmit(body);
	};
	return (
		<Sheet onOpenChange={onOpenChange} open={open}>
			<SheetContent className="sm:max-w-md">
				<form onSubmit={submit}>
					<SheetHeader>
						<SheetTitle>Retarget goal</SheetTitle>
						<SheetDescription>Update the card goal and hand off to Hermes while keeping the card running.</SheetDescription>
					</SheetHeader>
					<div className="space-y-4 px-4 py-4">
						<div className="space-y-2">
							<Label htmlFor={titleId}>Title</Label>
							<Input id={titleId} onChange={(event) => setTitle(event.target.value)} value={title} />
						</div>
						<div className="space-y-2">
							<Label htmlFor={notesId}>Notes</Label>
							<Input id={notesId} onChange={(event) => setNotes(event.target.value)} value={notes} />
						</div>
					</div>
					<SheetFooter>
						<Button disabled={pending} type="submit">Retarget</Button>
					</SheetFooter>
				</form>
			</SheetContent>
		</Sheet>
	);
}

function SplitSheet({
	card,
	open,
	onOpenChange,
	pending,
	onSubmit,
}: {
	card: WorkboardCard;
	open: boolean;
	onOpenChange: (open: boolean) => void;
	pending: boolean;
	onSubmit: (body: SplitWorkCardRequest) => void;
}) {
	const titleId = useId();
	const notesId = useId();
	const fateId = useId();
	const [title, setTitle] = useState(`${card.title} (split)`);
	const [notes, setNotes] = useState(card.notes);
	const [oldCardFate, setOldCardFate] = useState<SplitFate>("todo");
	const [startImmediately, setStartImmediately] = useState(true);
	const submit = (event: FormEvent<HTMLFormElement>) => {
		event.preventDefault();
		if (pending) return;
		onSubmit({ title: title.trim(), notes: notes.trim(), oldCardFate, startImmediately });
	};
	return (
		<Sheet onOpenChange={onOpenChange} open={open}>
			<SheetContent className="sm:max-w-md">
				<form onSubmit={submit}>
					<SheetHeader>
						<SheetTitle>Split card</SheetTitle>
						<SheetDescription>Create a successor card from the same base and archive this running card.</SheetDescription>
					</SheetHeader>
					<div className="space-y-4 px-4 py-4">
						<div className="space-y-2">
							<Label htmlFor={titleId}>New card title</Label>
							<Input id={titleId} onChange={(event) => setTitle(event.target.value)} value={title} />
						</div>
						<div className="space-y-2">
							<Label htmlFor={notesId}>New card notes</Label>
							<Input id={notesId} onChange={(event) => setNotes(event.target.value)} value={notes} />
						</div>
						<div className="space-y-2">
							<Label htmlFor={fateId}>Original card fate</Label>
							<Select onValueChange={(value) => setOldCardFate(value as SplitFate)} value={oldCardFate}>
								<SelectTrigger id={fateId}><SelectValue /></SelectTrigger>
								<SelectContent>
									<SelectItem value="todo">To do (recommended)</SelectItem>
									<SelectItem value="blocked">Blocked</SelectItem>
									<SelectItem value="done">Done</SelectItem>
								</SelectContent>
							</Select>
						</div>
						<label className="flex items-center gap-2 text-[12px] text-muted-foreground">
							<input checked={startImmediately} onChange={(event) => setStartImmediately(event.target.checked)} type="checkbox" />
							Start successor immediately (ready)
						</label>
					</div>
					<SheetFooter>
						<Button disabled={pending || !title.trim() || !notes.trim()} type="submit">Split card</Button>
					</SheetFooter>
				</form>
			</SheetContent>
		</Sheet>
	);
}
