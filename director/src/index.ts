import { spawn } from "node:child_process";
import { createDeepAgent } from "deepagents";
import { tool } from "@langchain/core/tools";
import { z } from "zod";
import { loadADHDSkill } from "./adhd.js";
import { IterationBudget } from "./budget.js";
import { loadConfig } from "./config.js";
import { loadGitWorkflowSkill } from "./git_workflow.js";
import { inboundInstruction } from "./inbound.js";
import { createDirectorModel } from "./model.js";
import { directorSystemPrompt } from "./prompt.js";
import {
	buildSendWorkerAnswerArgv,
	buildShowCardArgv,
	buildSpawnWorkerArgv,
	buildTransitionArgv,
	runAo,
	type Runner,
} from "./tools.js";

/** Runs `ao` from PATH. The session manager pins the daemon's own directory
 * onto PATH, so a bare `ao` resolves to the daemon that spawned this session. */
const runner: Runner = (argv) =>
	new Promise((resolve, reject) => {
		const child = spawn("ao", argv, { stdio: ["ignore", "pipe", "pipe"] });
		let stdout = "";
		let stderr = "";
		child.stdout.on("data", (c) => {
			stdout += String(c);
		});
		child.stderr.on("data", (c) => {
			stderr += String(c);
		});
		child.on("error", reject);
		child.on("close", (code) => resolve({ code: code ?? 1, stdout, stderr }));
	});

async function main(): Promise<void> {
	const cfg = loadConfig(process.env);
	const [adhdSkill, gitWorkflowSkill] = await Promise.all([
		loadADHDSkill(process.env),
		loadGitWorkflowSkill(process.env),
	]);
	const budget = new IterationBudget(cfg.maxIterations);
	let finish: (() => void) | undefined;
	const finished = new Promise<void>((resolve) => {
		finish = resolve;
	});

	const showCard = tool(
		async () => runAo(runner, buildShowCardArgv(cfg.cardId)),
		{
			name: "show_card",
			description: "Read the current state of the work card you own, as JSON.",
			schema: z.object({}),
		},
	);

	let terminalCard = false;
	const transitionCard = tool(
		async ({ to, reason }: { to: string; reason: string }) => {
			const output = await runAo(runner, buildTransitionArgv(cfg.cardId, to, reason));
			terminalCard = to === "done" || to === "blocked";
			return output;
		},
		{
			name: "transition_card",
			description:
				"Move the card to a new status (review, testing, done, redo, blocked). Always supply a reason explaining why.",
			schema: z.object({
				to: z.string().describe("Target status: review, testing, done, redo, or blocked"),
				reason: z.string().describe("Why the card is moving, or the exact blocker if blocking"),
			}),
		},
	);

	const spawnWorker = tool(
		async ({ agent, prompt }: { agent: string; prompt: string }) =>
			runAo(
				runner,
				buildSpawnWorkerArgv(
					agent,
					`${prompt.trim()}\n\nYou are supervised by Director session ${cfg.sessionId}. If your CLI asks a question or you are blocked, send the exact question to the Director with: ao send --session ${cfg.sessionId} --message "<question>". Wait for its answer before proceeding. Before completing your assigned phase, record a durable handoff with \`ao workboard card handoff <card-id> --phase <coding|review|testing> --summary "<what you did or found>" --changed <file> --check "<command and result>" --commit <sha-or-pr> --next "<what the next phase must verify>". Then send the same concise report to the Director with \`ao send --session ${cfg.sessionId} --message "Handoff: <report>"\`.`,
				),
			),
		{
			name: "spawn_worker",
			description: "Delegate an implementation subtask to a worker agent session.",
			schema: z.object({
				agent: z.string().describe("Harness to run the worker on, e.g. claude-code"),
				prompt: z.string().describe("The exact subtask, including the card id"),
			}),
		},
	);

	const answerWorker = tool(
		async ({ sessionId, answer }: { sessionId: string; answer: string }) =>
			runAo(runner, buildSendWorkerAnswerArgv(sessionId, answer)),
		{
			name: "answer_worker",
			description: "Answer a worker's question by sending the decision into its live agent CLI terminal.",
			schema: z.object({
				sessionId: z.string().describe("The worker session that asked the question"),
				answer: z.string().describe("A clear, actionable answer for that worker"),
			}),
		},
	);

	const agent = await createDeepAgent({
		model: createDirectorModel(cfg.model, process.env),
		tools: [showCard, transitionCard, spawnWorker, answerWorker],
		systemPrompt: [directorSystemPrompt(cfg.cardId), adhdSkill, gitWorkflowSkill].filter(Boolean).join("\n\n"),
	});

	let messages: Array<{ role: string; content: string }> = [];
	const drive = async (instruction: string): Promise<void> => {
		messages = [...messages, { role: "user", content: instruction }];
		while (true) {
		if (budget.shouldForceBlock()) {
			// Enforced, not requested: stop and record why while budget remains.
			await runAo(
				runner,
				buildTransitionArgv(
					cfg.cardId,
					"blocked",
					`Director stopped with ${budget.remaining} iterations left in its budget. Last state: ${summarize(messages)}`,
				),
			);
			terminalCard = true;
			finish?.();
			return;
		}
		budget.recordIteration();
		// DeepAgent's TypeScript declaration does not expose invoke at the top
		// level (it is inherited from ReactAgent via a merged interface declaration).
		// The runtime supports it; use an explicit cast so TypeScript accepts it.
		type DeepAgentInvoke = {
			invoke(input: {
				messages: Array<{ role: string; content: string }>;
			}): Promise<{ messages?: Array<{ tool_calls?: unknown[] }> }>;
		};
		const result = await (agent as unknown as DeepAgentInvoke).invoke({ messages });
		messages = (result.messages ?? []) as typeof messages;
		if (terminalCard) {
			finish?.();
			return;
		}
		if (isFinished(result)) return;
		}
	};

	let queue = Promise.resolve();
	const enqueue = (instruction: string) => {
		queue = queue.then(() => drive(instruction)).catch((err: unknown) => {
			console.error(`director: ${err instanceof Error ? err.message : String(err)}`);
		});
	};

	const opening = cfg.prompt.trim() || `Drive work card ${cfg.cardId} to completion.`;
	enqueue(opening);

	// The daemon's session messenger writes to this process's PTY. Keeping stdin
	// open turns the Director terminal into a control channel: a worker's
	// `ao send` message is a new model turn, not text lost at a shell prompt.
	//
	// One `ao send` arrives as several stdin chunks (tmux send-keys is chunked,
	// then Enter). Buffer until the message stops arriving, so a multi-line
	// handoff report reaches the model whole instead of one turn per line.
	//
	// ponytail: a fixed quiet window, not a framed protocol. The ceiling is a
	// message that stalls mid-flight for longer than the window arriving as two
	// turns. Upgrade path: have `ao send` frame messages with an explicit
	// terminator the Director splits on.
	const INBOUND_QUIET_MS = 300;
	process.stdin.setEncoding("utf8");
	let pendingInput = "";
	let flushTimer: NodeJS.Timeout | undefined;
	const flushInbound = () => {
		const message = pendingInput.trim();
		pendingInput = "";
		if (message !== "" && !terminalCard) enqueue(inboundInstruction(message));
	};
	process.stdin.on("data", (chunk: string) => {
		pendingInput += chunk;
		if (flushTimer) clearTimeout(flushTimer);
		flushTimer = setTimeout(flushInbound, INBOUND_QUIET_MS);
	});
	process.stdin.resume();
	await finished;
	process.stdin.pause();
}

/** DeepAgents returns when the model stops requesting tools; treat the absence
 * of a pending tool call as the run being complete. */
function isFinished(
	result: { messages?: Array<{ tool_calls?: unknown[] }> },
): boolean {
	const msgs = result.messages ?? [];
	const last = msgs[msgs.length - 1];
	return !last?.tool_calls || last.tool_calls.length === 0;
}

function summarize(
	messages: Array<{ role: string; content: string }>,
): string {
	const last = messages[messages.length - 1];
	return (last?.content ?? "").slice(0, 500);
}

main().catch((err: unknown) => {
	console.error(
		`director: ${err instanceof Error ? err.message : String(err)}`,
	);
	process.exitCode = 1;
});
