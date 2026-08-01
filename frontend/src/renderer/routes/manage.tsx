import { createFileRoute } from "@tanstack/react-router";
import { Workboard } from "../components/Workboard";

export const Route = createFileRoute("/manage")({
	component: ManagePage,
});

// Code Manage mode: light shell — the root ModeBar sits above; the board owns
// the rest of the viewport. No project sidebar, no ShellTopbar (approved
// design: modes operate independently, board-first).
export function ManagePage() {
	return (
		<div className="h-full min-h-0 overflow-auto">
			<Workboard />
		</div>
	);
}
