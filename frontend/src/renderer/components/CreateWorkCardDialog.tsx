import * as Dialog from "@radix-ui/react-dialog";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FolderOpen, Loader2, Plus, X } from "lucide-react";
import { type FormEvent, type KeyboardEvent, useEffect, useId, useState } from "react";
import type { components } from "../../api/schema";
import { agentsQueryOptions } from "../hooks/useAgentsQuery";
import { workboardQueryKey, type WorkCard } from "../hooks/useWorkboardQuery";
import { apiClient, apiErrorMessage } from "../lib/api-client";
import { aoBridge } from "../lib/bridge";
import { defaultScheduleValue, parseDatetimeLocalValue } from "../lib/workboard-schedule";
import { RequiredAgentField } from "./CreateProjectAgentSheet";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Label } from "./ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./ui/select";

type CreateWorkCardRequest = components["schemas"]["CreateWorkCardRequest"];
type ProjectSummary = components["schemas"]["ProjectSummary"];

type CreateWorkCardDialogProps = {
	open: boolean;
	projectId?: string;
	onCreated?: (card: WorkCard) => void;
	onOpenChange: (open: boolean) => void;
};

export function CreateWorkCardDialog({ open, projectId: projectIdProp, onCreated, onOpenChange }: CreateWorkCardDialogProps) {
	const queryClient = useQueryClient();
	const titleId = useId();
	const notesId = useId();
	const projectIdId = useId();
	const folderId = useId();
	const labelsId = useId();
	const priorityId = useId();
	const agentId = useId();
	const reviewerModeId = useId();
	const reviewerAgentId = useId();
	const testingAgentId = useId();
	const scheduleToggleId = useId();
	const scheduleLabelId = useId();
	const scheduledAtId = useId();

	const [selectedProjectId, setSelectedProjectId] = useState(projectIdProp ?? "");
	const [title, setTitle] = useState("");
	const [notes, setNotes] = useState("");
	const [targetPath, setTargetPath] = useState("");
	const [labels, setLabels] = useState<string[]>([]);
	const [labelInput, setLabelInput] = useState("");
	const [priority, setPriority] = useState<CreateWorkCardRequest["priority"]>("normal");
	const [agent, setAgent] = useState("");
	const [reviewerMode, setReviewerMode] = useState<"same" | "separate">("same");
	const [reviewerAgent, setReviewerAgent] = useState("");
	const [testingAgent, setTestingAgent] = useState("");
	const [scheduleEnabled, setScheduleEnabled] = useState(false);
	const [scheduledAtLocal, setScheduledAtLocal] = useState(defaultScheduleValue);
	const [error, setError] = useState<string>();

	const projectsQuery = useQuery({
		queryKey: ["projects"],
		enabled: open,
		queryFn: async () => {
			const { data, error: apiError } = await apiClient.GET("/api/v1/projects");
			if (apiError) throw new Error(apiErrorMessage(apiError, "Could not load projects."));
			return data?.projects ?? [];
		},
	});

	const agentsQuery = useQuery({ ...agentsQueryOptions, enabled: open });

	const effectiveProjectId = selectedProjectId || projectIdProp || "";

	const createCard = useMutation({
		mutationFn: async (body: CreateWorkCardRequest) => {
			if (projectIdProp) {
				const { data, error: apiError } = await apiClient.POST("/api/v1/projects/{projectId}/workboard/cards", {
					params: { path: { projectId: projectIdProp } },
					body,
				});
				if (apiError) throw new Error(apiErrorMessage(apiError, "Could not create work card."));
				if (!data) throw new Error("Work card creation returned no card.");
				return data as WorkCard;
			}
			const { data, error: apiError } = await apiClient.POST("/api/v1/workboard/cards", {
				body: { ...body, projectId: effectiveProjectId },
			});
			if (apiError) throw new Error(apiErrorMessage(apiError, "Could not create work card."));
			if (!data) throw new Error("Work card creation returned no card.");
			return data as WorkCard;
		},
		onSuccess: async (card) => {
			await queryClient.invalidateQueries({ queryKey: workboardQueryKey(projectIdProp) });
			await queryClient.invalidateQueries({ queryKey: workboardQueryKey() });
			onCreated?.(card);
			onOpenChange(false);
		},
		onError: (nextError) => setError(nextError instanceof Error ? nextError.message : "Could not create work card."),
	});

	useEffect(() => {
		if (!open) {
			setSelectedProjectId(projectIdProp ?? "");
			setTitle("");
			setNotes("");
			setTargetPath("");
			setLabels([]);
			setLabelInput("");
			setPriority("normal");
			setAgent("");
			setReviewerMode("same");
			setReviewerAgent("");
			setTestingAgent("");
			setScheduleEnabled(false);
			setScheduledAtLocal(defaultScheduleValue());
			setError(undefined);
		}
	}, [open, projectIdProp]);

	// In the global Director board there is no projectIdProp, so the user would
	// otherwise have to pick a project before they can create a card. When there
	// is only one registered project, auto-select it so the user can go straight
	// to title + agent.
	useEffect(() => {
		if (projectIdProp) return;
		if (selectedProjectId) return;
		const projects = projectsQuery.data;
		if (!projects || projects.length !== 1) return;
		const only = projects[0];
		setSelectedProjectId(only.id);
		if (only.path) setTargetPath(only.path);
	}, [projectIdProp, selectedProjectId, projectsQuery.data]);

	const handleProjectChange = (projId: string) => {
		setSelectedProjectId(projId);
		const proj = projectsQuery.data?.find((p: ProjectSummary) => p.id === projId);
		if (proj?.path) {
			setTargetPath(proj.path);
		}
	};

	const addLabel = () => {
		const next = labelInput.trim().replace(/,$/, "");
		if (next && !labels.includes(next)) setLabels((current) => [...current, next]);
		setLabelInput("");
	};
	const handleLabelKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
		if (event.key !== "Enter" && event.key !== ",") return;
		event.preventDefault();
		addLabel();
	};
	const chooseFolder = async () => {
		setError(undefined);
		try {
			const nextPath = await aoBridge.app.chooseDirectory();
			if (nextPath) setTargetPath(nextPath);
		} catch (nextError) {
			setError(nextError instanceof Error ? nextError.message : "Could not choose a folder.");
		}
	};

	const submit = (event: FormEvent<HTMLFormElement>) => {
		event.preventDefault();
		if (createCard.isPending) return;
		if (!effectiveProjectId) {
			setError("Select a project before creating this card.");
			return;
		}
		const cleanTitle = title.trim();
		const cleanNotes = notes.trim();
		const cleanPath = targetPath.trim();
		if (!cleanTitle) {
			setError("Title is required.");
			return;
		}
		if (!agent) {
			setError("Select an agent before creating this card.");
			return;
		}
		const pendingLabel = labelInput.trim().replace(/,$/, "");
		const nextLabels = pendingLabel && !labels.includes(pendingLabel) ? [...labels, pendingLabel] : labels;

		let scheduledAt: string | undefined;
		if (scheduleEnabled) {
			scheduledAt = parseDatetimeLocalValue(scheduledAtLocal);
			if (!scheduledAt) {
				setError("Choose a valid schedule time.");
				return;
			}
		}
		setLabels(nextLabels);
		setLabelInput("");
		createCard.mutate({
			projectId: effectiveProjectId,
			title: cleanTitle,
			notes: cleanNotes,
			targetPath: cleanPath || undefined,
			labels: nextLabels,
			priority,
			agent,
			codingAgent: agent,
			reviewerMode,
			reviewerAgent: reviewerMode === "separate" ? reviewerAgent : undefined,
			testingAgent: testingAgent || undefined,
			...(scheduleEnabled ? { status: "scheduled" as const, scheduledAt } : {}),
		});
	};

	return (
		<Dialog.Root open={open} onOpenChange={(next) => !createCard.isPending && onOpenChange(next)}>
			<Dialog.Portal>
				<Dialog.Overlay className="fixed inset-0 z-50 bg-black/55 motion-reduce:animate-none data-[state=open]:animate-overlay-in" />
				<Dialog.Content className="fixed left-1/2 top-1/2 z-50 w-[min(640px,calc(100vw-32px))] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-popover p-0 text-popover-foreground shadow-xl motion-reduce:animate-none data-[state=open]:animate-modal-in max-h-[90vh] overflow-y-auto">
					<div className="flex items-start justify-between gap-4 border-b border-border px-5 py-4">
						<div className="min-w-0">
							<Dialog.Title className="text-[15px] font-semibold text-foreground">Create work card</Dialog.Title>
							<Dialog.Description className="mt-1 text-[12px] text-muted-foreground">Select a project, set the goal, and configure agents for this work.</Dialog.Description>
						</div>
						<Dialog.Close asChild>
							<button aria-label="Close create work card dialog" className="grid size-7 shrink-0 place-items-center rounded-md text-muted-foreground transition hover:bg-surface hover:text-foreground motion-reduce:transition-none" type="button">
								<X className="size-4" aria-hidden="true" />
							</button>
						</Dialog.Close>
					</div>
					<form className="space-y-4 px-5 py-4" onSubmit={submit}>
						<div className="grid gap-3 sm:grid-cols-2">
							<div className="space-y-1.5">
								<Label htmlFor={projectIdId}>Project *</Label>
								{projectIdProp ? (
									<Input disabled id={projectIdId} value={projectIdProp} />
								) : (
									<Select value={selectedProjectId} onValueChange={handleProjectChange}>
										<SelectTrigger id={projectIdId} className="h-8 w-full text-[13px]">
											<SelectValue placeholder="Select project" />
										</SelectTrigger>
										<SelectContent>
											{(projectsQuery.data ?? []).map((p: ProjectSummary) => (
												<SelectItem key={p.id} value={p.id}>
													{p.name || p.id} ({p.path})
												</SelectItem>
											))}
										</SelectContent>
									</Select>
								)}
							</div>
							<div className="space-y-1.5">
								<Label htmlFor={priorityId}>Priority</Label>
								<Select value={priority} onValueChange={(value) => setPriority(value as CreateWorkCardRequest["priority"])}>
									<SelectTrigger id={priorityId} className="h-8 w-full text-[13px]"><SelectValue /></SelectTrigger>
									<SelectContent>{(["low", "normal", "high", "urgent"] as const).map((value) => <SelectItem key={value} value={value}>{value[0].toUpperCase() + value.slice(1)}</SelectItem>)}</SelectContent>
								</Select>
							</div>
						</div>
						<div className="space-y-1.5">
							<Label htmlFor={titleId}>Title *</Label>
							<Input autoFocus id={titleId} onChange={(event) => setTitle(event.target.value)} placeholder="Repair build diagnostics" value={title} />
						</div>
						<div className="space-y-1.5">
							<Label htmlFor={notesId}>Notes</Label>
							<textarea id={notesId} className="min-h-[84px] w-full resize-y rounded-md border border-border bg-transparent px-3 py-2 text-[13px] leading-relaxed text-foreground outline-none transition placeholder:text-passive focus-visible:border-accent focus-visible:ring-2 focus-visible:ring-accent-weak motion-reduce:transition-none" onChange={(event) => setNotes(event.target.value)} placeholder="Describe the outcome and constraints for the coding agent." value={notes} />
						</div>
						<div className="space-y-1.5">
							<Label htmlFor={folderId}>Folder</Label>
							<div className="flex gap-2">
								<Input id={folderId} onChange={(event) => setTargetPath(event.target.value)} placeholder="Path inside project repo" value={targetPath} />
								<Button aria-label="Choose folder" onClick={() => void chooseFolder()} size="icon" type="button" variant="outline"><FolderOpen className="size-3.5" aria-hidden="true" /></Button>
							</div>
						</div>
						<div className="space-y-1.5">
							<Label htmlFor={labelsId}>Labels</Label>
							<div className="min-h-8 rounded-md border border-border bg-transparent px-2 py-1 focus-within:border-accent focus-within:ring-2 focus-within:ring-accent-weak">
								<div className="flex flex-wrap items-center gap-1">
									{labels.map((label) => <button aria-label={`Remove ${label} label`} className="rounded-[3px] bg-raised px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground hover:text-foreground" key={label} onClick={() => setLabels((current) => current.filter((item) => item !== label))} type="button">{label} ×</button>)}
									<input aria-label="Labels" className="min-w-[8rem] flex-1 bg-transparent px-1 py-0.5 text-[12px] text-foreground outline-none placeholder:text-passive" id={labelsId} onChange={(event) => setLabelInput(event.target.value)} onKeyDown={handleLabelKeyDown} placeholder={labels.length ? "Add label" : "Type then Enter"} value={labelInput} />
								</div>
							</div>
						</div>
						<div className="rounded-md border border-border p-3 space-y-3">
							<h4 className="text-[12px] font-semibold text-foreground">Agent Configuration</h4>
							<div className="grid gap-3 sm:grid-cols-2">
								<RequiredAgentField authorized={agentsQuery.data?.authorized} disabled={agentsQuery.isFetching && !agentsQuery.data} id={agentId} installed={agentsQuery.data?.installed} invalid={Boolean(error) && !agent} label="Coding Agent *" onChange={setAgent} placeholder="Select coding agent" supported={agentsQuery.data?.supported} value={agent} />
								<div className="space-y-1.5">
									<Label htmlFor={reviewerModeId}>Reviewer Mode</Label>
									<Select value={reviewerMode} onValueChange={(val) => setReviewerMode(val as "same" | "separate")}>
										<SelectTrigger id={reviewerModeId} className="h-8 w-full text-[13px]"><SelectValue /></SelectTrigger>
										<SelectContent>
											<SelectItem value="same">Same Coding Agent</SelectItem>
											<SelectItem value="separate">Separate Reviewer Agent</SelectItem>
										</SelectContent>
									</Select>
								</div>
							</div>
							{reviewerMode === "separate" ? (
								<RequiredAgentField authorized={agentsQuery.data?.authorized} disabled={agentsQuery.isFetching && !agentsQuery.data} id={reviewerAgentId} installed={agentsQuery.data?.installed} label="Reviewer Agent" onChange={setReviewerAgent} placeholder="Select reviewer agent" supported={agentsQuery.data?.supported} value={reviewerAgent} />
							) : null}
							<RequiredAgentField authorized={agentsQuery.data?.authorized} disabled={agentsQuery.isFetching && !agentsQuery.data} id={testingAgentId} installed={agentsQuery.data?.installed} label="Testing Agent (Optional)" onChange={setTestingAgent} placeholder="Select testing agent" supported={agentsQuery.data?.supported} value={testingAgent} />
						</div>
						<div className="space-y-2 rounded-md border border-border px-3 py-3">
							<label className="flex items-start gap-3 text-[13px]" htmlFor={scheduleToggleId}>
								<input aria-labelledby={scheduleLabelId} checked={scheduleEnabled} className="mt-0.5 accent-[var(--accent)]" id={scheduleToggleId} onChange={(event) => setScheduleEnabled(event.target.checked)} type="checkbox" />
								<span id={scheduleLabelId}><span className="block font-medium text-foreground">Schedule for later</span><span className="mt-1 block text-[12px] text-muted-foreground">Place the card in Scheduled until promoted automatically.</span></span>
							</label>
							{scheduleEnabled ? (
								<div className="space-y-1.5 pl-7">
									<Label htmlFor={scheduledAtId}>Run at</Label>
									<Input id={scheduledAtId} onChange={(event) => setScheduledAtLocal(event.target.value)} type="datetime-local" value={scheduledAtLocal} />
								</div>
							) : null}
						</div>
						{error ? <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-[12px] text-destructive" role="alert">{error}</div> : null}
						<div className="flex items-center justify-end gap-2 pt-1">
							<Dialog.Close asChild><Button disabled={createCard.isPending} type="button" variant="ghost">Cancel</Button></Dialog.Close>
							<Button disabled={createCard.isPending} type="submit">{createCard.isPending ? <Loader2 className="size-3.5 animate-spin motion-reduce:animate-none" aria-hidden="true" /> : <Plus className="size-3.5" aria-hidden="true" />}{createCard.isPending ? "Creating..." : "Create card"}</Button>
						</div>
					</form>
				</Dialog.Content>
			</Dialog.Portal>
		</Dialog.Root>
	);
}
