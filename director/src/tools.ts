export type Runner = (argv: string[]) => Promise<{ code: number; stdout: string; stderr: string }>;

export function buildShowCardArgv(cardId: string): string[] {
	return ["workboard", "card", "show", cardId, "--json"];
}

export function buildTransitionArgv(cardId: string, to: string, reason: string): string[] {
	if (reason.trim() === "") {
		// A blocked card with no reason is the failure this agent exists to avoid:
		// it tells the human nothing about what it needs.
		throw new Error("a transition reason is required");
	}
	return ["workboard", "card", "transition", cardId, "--to", to, "--reason", reason];
}

export function buildSpawnWorkerArgv(projectId: string, agent: string, prompt: string): string[] {
	return ["spawn", "--project", projectId, "--agent", agent, "--prompt", prompt];
}

export function buildSendWorkerAnswerArgv(sessionId: string, answer: string): string[] {
	if (sessionId.trim() === "") throw new Error("a worker session id is required");
	if (answer.trim() === "") throw new Error("an answer is required");
	return ["send", "--session", sessionId, "--message", answer];
}

/** Runs an `ao` subcommand, returning stdout. A non-zero exit becomes a thrown
 * error so DeepAgents surfaces it to the model as a tool failure rather than
 * letting the agent proceed on a silently failed action. */
export async function runAo(run: Runner, argv: string[]): Promise<string> {
	const { code, stdout, stderr } = await run(argv);
	if (code !== 0) {
		throw new Error(`ao ${argv.join(" ")} failed (exit ${code}): ${stderr.trim() || stdout.trim()}`);
	}
	return stdout;
}
