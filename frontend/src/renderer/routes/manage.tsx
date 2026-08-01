import { createFileRoute } from "@tanstack/react-router";
import { Workboard } from "../components/Workboard";
import { useDaemonStatus } from "../hooks/useDaemonStatus";
import { ShellProvider } from "../lib/shell-context";

export const Route = createFileRoute("/manage")({
	component: ManagePage,
});

// Code Manage mode: light shell — the root ModeBar sits above; the board owns
// the rest of the viewport. No project sidebar, no ShellTopbar (approved
// design: modes operate independently, board-first).
export function ManagePage() {
	const daemonStatus = useDaemonStatus();

	return (
		<ShellProvider
			value={{
				daemonStatus,
				// Code Manage mode operates without project creation; provide a no-op
				// that throws if invoked. Workboard only calls this in project-scoped
				// views (via WorkCardFocusPanel.projectId), which don't reach here.
				createProject: () => {
					throw new Error("createProject is not available in Code Manage mode");
				},
			}}
		>
			<div className="h-full min-h-0 overflow-auto">
				<Workboard />
			</div>
		</ShellProvider>
	);
}
