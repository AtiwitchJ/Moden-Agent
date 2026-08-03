import { spawn } from "node:child_process";
import { createDeepAgent } from "deepagents";
import { tool } from "@langchain/core/tools";
import { z } from "zod";
import { IterationBudget } from "./budget.js";
import { loadConfig } from "./config.js";
import { directorSystemPrompt } from "./prompt.js";
import { buildShowCardArgv, buildSpawnWorkerArgv, buildTransitionArgv, runAo, } from "./tools.js";
/** Runs `ao` from PATH. The session manager pins the daemon's own directory
 * onto PATH, so a bare `ao` resolves to the daemon that spawned this session. */
const runner = (argv) => new Promise((resolve, reject) => {
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
async function main() {
    const cfg = loadConfig(process.env);
    const budget = new IterationBudget(cfg.maxIterations);
    const showCard = tool(async () => runAo(runner, buildShowCardArgv(cfg.cardId)), {
        name: "show_card",
        description: "Read the current state of the work card you own, as JSON.",
        schema: z.object({}),
    });
    const transitionCard = tool(async ({ to, reason }) => runAo(runner, buildTransitionArgv(cfg.cardId, to, reason)), {
        name: "transition_card",
        description: "Move the card to a new status (review, testing, done, redo, blocked). Always supply a reason explaining why.",
        schema: z.object({
            to: z.string().describe("Target status: review, testing, done, redo, or blocked"),
            reason: z.string().describe("Why the card is moving, or the exact blocker if blocking"),
        }),
    });
    const spawnWorker = tool(async ({ agent, prompt }) => runAo(runner, buildSpawnWorkerArgv(agent, prompt)), {
        name: "spawn_worker",
        description: "Delegate an implementation subtask to a worker agent session.",
        schema: z.object({
            agent: z.string().describe("Harness to run the worker on, e.g. claude-code"),
            prompt: z.string().describe("The exact subtask, including the card id"),
        }),
    });
    const agent = await createDeepAgent({
        model: cfg.model,
        tools: [showCard, transitionCard, spawnWorker],
        systemPrompt: directorSystemPrompt(cfg.cardId),
    });
    const opening = cfg.prompt.trim() || `Drive work card ${cfg.cardId} to completion.`;
    let messages = [
        { role: "user", content: opening },
    ];
    while (true) {
        if (budget.shouldForceBlock()) {
            // Enforced, not requested: stop and record why while budget remains.
            await runAo(runner, buildTransitionArgv(cfg.cardId, "blocked", `Director stopped with ${budget.remaining} iterations left in its budget. Last state: ${summarize(messages)}`));
            return;
        }
        budget.recordIteration();
        const result = await agent.invoke({ messages });
        messages = (result.messages ?? []);
        if (isFinished(result))
            return;
    }
}
/** DeepAgents returns when the model stops requesting tools; treat the absence
 * of a pending tool call as the run being complete. */
function isFinished(result) {
    const msgs = result.messages ?? [];
    const last = msgs[msgs.length - 1];
    return !last?.tool_calls || last.tool_calls.length === 0;
}
function summarize(messages) {
    const last = messages[messages.length - 1];
    return (last?.content ?? "").slice(0, 500);
}
main().catch((err) => {
    console.error(`director: ${err instanceof Error ? err.message : String(err)}`);
    process.exitCode = 1;
});
