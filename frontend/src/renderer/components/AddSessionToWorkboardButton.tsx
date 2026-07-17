import { useMutation, useQueryClient } from "@tanstack/react-query";
import { LayoutGrid, Loader2 } from "lucide-react";
import { useNavigate } from "@tanstack/react-router";
import { addSessionToWorkboard, sessionCanJoinWorkboard, sessionLinkedWorkCard } from "../lib/add-session-to-workboard";
import { workboardQueryKey, useWorkboardCards } from "../hooks/useWorkboardQuery";
import { useWorkspaceQuery } from "../hooks/useWorkspaceQuery";
import type { WorkspaceSession } from "../types/workspace";
import { Button } from "./ui/button";
import { cn } from "../lib/utils";

type AddSessionToWorkboardButtonProps = {
	session: WorkspaceSession;
	className?: string;
	size?: "default" | "sm" | "icon";
	variant?: "primary" | "outline" | "secondary" | "ghost";
	showLabel?: boolean;
};

export function AddSessionToWorkboardButton({
	session,
	className,
	size = "sm",
	variant = "outline",
	showLabel = true,
}: AddSessionToWorkboardButtonProps) {
	const navigate = useNavigate();
	const queryClient = useQueryClient();
	const workspaceQuery = useWorkspaceQuery();
	const workspace = workspaceQuery.data?.find((item) => item.id === session.workspaceId);
	const cardsQuery = useWorkboardCards(session.workspaceId);
	const linkedCard = sessionLinkedWorkCard(cardsQuery.data, session.id);
	const canAdd =
		Boolean(workspace) &&
		sessionCanJoinWorkboard(session) &&
		cardsQuery.isFetched &&
		!cardsQuery.isLoading &&
		!linkedCard;

	const addToWorkboard = useMutation({
		mutationFn: async () => {
			if (!workspace) throw new Error("Project not found.");
			return addSessionToWorkboard(session, workspace);
		},
		onSuccess: async () => {
			await queryClient.invalidateQueries({ queryKey: workboardQueryKey(session.workspaceId) });
			void navigate({ to: "/projects/$projectId", params: { projectId: session.workspaceId } });
		},
	});

	if (!cardsQuery.isFetched || cardsQuery.isLoading) {
		return null;
	}

	if (!canAdd) {
		if (linkedCard) {
			return (
				<Button
					aria-label="Open linked work card"
					className={cn("gap-1.5", className)}
					onClick={() => void navigate({ to: "/projects/$projectId", params: { projectId: session.workspaceId } })}
					size={size}
					type="button"
					variant={variant}
				>
					<LayoutGrid className="size-3.5" aria-hidden="true" />
					{showLabel ? "Open work card" : null}
				</Button>
			);
		}
		return null;
	}

	return (
		<div className={cn("flex flex-col items-start gap-1", className)}>
			<Button
				aria-label={`Add ${session.title} to Workboard`}
				className="gap-1.5"
				disabled={addToWorkboard.isPending}
				onClick={(event) => {
					event.stopPropagation();
					addToWorkboard.mutate();
				}}
				size={size}
				type="button"
				variant={variant}
			>
				{addToWorkboard.isPending ? (
					<Loader2 className="size-3.5 animate-spin motion-reduce:animate-none" aria-hidden="true" />
				) : (
					<LayoutGrid className="size-3.5" aria-hidden="true" />
				)}
				{showLabel ? (addToWorkboard.isPending ? "Adding…" : "Add to Workboard") : null}
			</Button>
			{addToWorkboard.isError ? (
				<p className="text-[12px] text-error" role="alert">
					{addToWorkboard.error instanceof Error ? addToWorkboard.error.message : "Could not add session to Workboard."}
				</p>
			) : null}
		</div>
	);
}
