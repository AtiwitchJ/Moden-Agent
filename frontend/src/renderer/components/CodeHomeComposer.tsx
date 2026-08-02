import { useNavigate } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { useWorkspaceQuery } from "../hooks/useWorkspaceQuery";
import { useShell } from "../lib/shell-context";
import { spawnOrchestrator } from "../lib/spawn-orchestrator";
import type { CreateProjectAgentSelection } from "./CreateProjectAgentSheet";
import { CODE_HOME_COMPOSER_INPUT_ID } from "./CodeSidebar";
import { CreateProjectFlow } from "./Sidebar";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./ui/select";

const NEW_PROJECT_VALUE = "__new_project__";

// Primary session-creation surface for Code mode's home: pick a project (or
// create one), type a first prompt, submit — spawns a session with the prompt
// as its first message. The daemon owns delivery timing (immediate for an
// already-running orchestrator, readiness-polled for a freshly spawned one —
// see session_manager.Manager.waitForOutputSteady); this composer only passes
// the prompt through spawnOrchestrator/createProject and never calls
// /sessions/{id}/send itself, so it can't race the agent's own boot. See
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

	useEffect(() => {
		inputRef.current?.focus();
	}, []);

	const onCreateProject = async (selection: CreateProjectAgentSelection & { path: string }) => {
		setError(null);
		setIsSubmitting(true);
		try {
			const result = await createProject({ ...selection, prompt: draft.trim() });
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
			const sessionId = await spawnOrchestrator(projectId, false, draft.trim());
			setDraft("");
			void navigate({ to: "/projects/$projectId/sessions/$sessionId", params: { projectId, sessionId } });
		} catch (err) {
			setError(err instanceof Error ? err.message : "Could not start session");
		} finally {
			setIsSubmitting(false);
		}
	};

	return (
		<CreateProjectFlow onCreateProject={onCreateProject} simple>
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
						id={CODE_HOME_COMPOSER_INPUT_ID}
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
