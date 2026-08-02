import { createContext, useContext } from "react";
import type { useDaemonStatus } from "../hooks/useDaemonStatus";

// Shared state the persistent _shell layout owns and route content reads. The
// daemon status effect (IPC poll + event transport) must run exactly once, so
// it lives in the shell and is handed down here rather than re-run per route.
export type ShellContextValue = {
	daemonStatus: ReturnType<typeof useDaemonStatus>;
	createProject: (input: {
		path: string;
		workerAgent: string;
		orchestratorAgent: string;
		trackerIntake?: any;
		companyId?: string;
		asWorkspace?: boolean;
		/** Delivered as the new session's first message. */
		prompt?: string;
		/** Code mode requests a coding worker; other creation paths use the
		 * project's orchestrator. */
		sessionKind?: "orchestrator" | "worker";
	}) => Promise<{ projectId: string; sessionId: string }>;
	removeProject: (projectId: string) => Promise<void>;
};

const ShellContext = createContext<ShellContextValue | null>(null);

export const ShellProvider = ShellContext.Provider;

export function useShell(): ShellContextValue {
	const ctx = useContext(ShellContext);
	if (!ctx) throw new Error("useShell must be used within the _shell layout route");
	return ctx;
}
