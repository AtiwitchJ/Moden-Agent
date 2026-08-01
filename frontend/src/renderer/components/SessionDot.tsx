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
