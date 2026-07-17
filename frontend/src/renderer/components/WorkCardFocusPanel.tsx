import { useMutation, useQueryClient } from "@tanstack/react-query";
import { MoreHorizontal, Split, Target, Zap } from "lucide-react";
import { type FormEvent, useId, useState } from "react";
import type { components } from "../../api/schema";
import { workboardQueryKey, type WorkCard as WorkboardCard } from "../hooks/useWorkboardQuery";
import { apiClient, apiErrorMessage } from "../lib/api-client";
import type { Theme } from "../stores/ui-store";
import type { WorkspaceSession } from "../types/workspace";
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
	const showTerminal = card.status === "running" && Boolean(card.sessionId && session);
	const isRunning = card.status === "running";

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

	return (
		<aside aria-label={`Focus panel for ${card.title}`} className="flex h-full w-[360px] shrink-0 flex-col border-l border-border bg-surface">
			<div className="flex shrink-0 items-start gap-3 border-b border-border px-4 py-3">
				<div className="min-w-0 flex-1">
					<div className="font-mono text-[10px] font-semibold uppercase tracking-[0.08em] text-accent">Focused card</div>
					<h2 className="mt-1 line-clamp-2 text-[14px] font-medium leading-[1.35] text-foreground">{card.title}</h2>
				</div>
				{isRunning ? (
					<DropdownMenu>
						<DropdownMenuTrigger asChild>
							<Button aria-label="Running card actions" size="icon-sm" variant="ghost">
								<MoreHorizontal className="size-4" aria-hidden="true" />
							</Button>
						</DropdownMenuTrigger>
						<DropdownMenuContent align="end" className="min-w-44">
							<DropdownMenuItem onSelect={() => setNudgeOpen(true)}>
								<Zap className="size-3.5" aria-hidden="true" />
								Nudge agent
							</DropdownMenuItem>
							<DropdownMenuItem onSelect={() => setRetargetOpen(true)}>
								<Target className="size-3.5" aria-hidden="true" />
								Retarget goal
							</DropdownMenuItem>
							<DropdownMenuItem onSelect={() => setSplitOpen(true)}>
								<Split className="size-3.5" aria-hidden="true" />
								Split card
							</DropdownMenuItem>
						</DropdownMenuContent>
					</DropdownMenu>
				) : null}
				<Button aria-label="Close card focus panel" onClick={onClose} size="icon-sm" variant="ghost">×</Button>
			</div>
			<div className="shrink-0 space-y-3 border-b border-border px-4 py-3">
				<div className="flex items-center justify-between gap-3 text-[11px]">
					<span className="text-muted-foreground">Status</span>
					<span className="font-mono uppercase tracking-[0.05em] text-foreground">{card.status}</span>
				</div>
				{card.sessionId ? (
					<div className="flex items-center gap-2 text-[11px] text-muted-foreground">
						<span className="truncate">{session ? `Session ${card.sessionId}` : "Linked session unavailable"}</span>
					</div>
				) : <p className="text-[11px] text-passive">No session is linked to this card yet.</p>}
				{card.sessionId && !showTerminal && onShowSessions ? <Button onClick={onShowSessions} size="sm" variant="outline">Open terminal</Button> : null}
				{actionError ? <p className="text-[11px] text-destructive" role="alert">{actionError}</p> : null}
			</div>
			{showTerminal ? (
				<div className="min-h-0 flex-1 p-3">
					<div className="h-full min-h-[260px] overflow-hidden rounded-md border border-border">
						<TerminalPane session={session} theme={theme} daemonReady={daemonReady} fontSize={TERMINAL_FONT_SIZE} />
					</div>
				</div>
			) : (
				<div className="flex flex-1 items-center justify-center px-8 text-center text-[12px] leading-[1.5] text-passive">
					{card.sessionId ? "Select a running card to preview its live terminal here." : "Select a card linked to a session to preview its terminal here."}
				</div>
			)}
			<NudgeSheet
				open={nudgeOpen}
				onOpenChange={setNudgeOpen}
				pending={nudge.isPending}
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

function NudgeSheet({
	open,
	onOpenChange,
	pending,
	onSubmit,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	pending: boolean;
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
						<SheetTitle>Nudge agent</SheetTitle>
						<SheetDescription>Send a message to the linked coding session without changing the card column.</SheetDescription>
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
