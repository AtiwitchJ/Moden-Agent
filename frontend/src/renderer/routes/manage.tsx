import { createFileRoute } from "@tanstack/react-router";
import { Workboard } from "../components/Workboard";
import { refreshDaemonStatus } from "../lib/daemon-status";

export const Route = createFileRoute("/manage")({
	loader: () => refreshDaemonStatus().catch(() => undefined),
	component: ManagePage,
});

// Director mode: light shell — the root ModeBar + TitlebarNav sit above;
// the board owns the rest of the viewport. No project sidebar, no ShellTopbar
// (approved design: modes operate independently, board-first).
// ShellProvider and daemonStatus are provided by root and inherited here.
export function ManagePage() {
	return <Workboard />;
}
