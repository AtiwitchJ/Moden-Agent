import { runAo, type Runner } from "./tools.js";

/** Tells the daemon this session's own agent process is exiting, on every
 * exit path — success, blocked, or a crash. The tmux pane survives after the
 * process exits (an `exec $SHELL` tail keeps it open for inspection), and
 * nothing else observes the exit while the daemon keeps running, so a stale,
 * still-"live" session row would otherwise block the project from ever
 * starting another Director. Best-effort: a failed report must never mask
 * the real reason the process is exiting. */
export async function reportExited(run: Runner): Promise<void> {
	try {
		await runAo(run, ["session", "mark-exited"]);
	} catch {
		// best-effort
	}
}
