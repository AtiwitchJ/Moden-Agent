# Director Agent Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the Director agent — a first-party TypeScript agent harness built on LangChain DeepAgents that AO owns end to end, so a project can drive work cards with an explicitly chosen engine (`openai:gpt-5`, `openrouter:minimax/minimax-m2`, `ollama:qwen2.5`, …) instead of whatever an external CLI happens to be configured for.

**Architecture:** Three layers, built bottom-up. (1) A new `director/` TypeScript package running the DeepAgents loop, whose tools shell out to the already-shipped `ao workboard card ...` CLI. (2) A new Go adapter `internal/adapters/agent/director/` registering `HarnessDirector = "director"`, which launches the built JS bundle — embedded in the daemon binary and installed into the data dir at boot, mirroring the existing `skillassets` pattern. (3) A `Config.Director` opt-in plus one additive branch in `DispatchOnce`, with CLI config plumbing and a frontend predicate fix so the feature is reachable and visible.

**Tech Stack:** Go 1.x (daemon, adapter, dispatch), TypeScript + Node 22 (`deepagents`, `langchain`, `@langchain/core`), vitest (director package tests), sqlc/SQLite (unchanged), chi (unchanged), React (frontend predicate change).

## Global Constraints

- **Do not modify, rename, or extend any existing Hermes-specific *backend* file or identifier.** `dispatch.go`'s `commanding` branch, `isHermesCommander` (Go, `service/workboard/actions.go`), `HermesSender`, `PrepareHermesAnswerAttempt`, `hermesWorkboardPrompt`, `hermesCardBriefing`, `stall_nudge.go`, `switch_agent.go` stay byte-for-byte identical.
- **Do not change the wire enum values** `hermes_unavailable` / `non_hermes_orchestrator` in `controllers/dto.go` or `openapi.yaml`. Display text may be neutralized; enum values may not.
- **Do not touch `internal/commander/orchestrator/*`** — a separate, already-wired system.
- Keep every change surgical and tied to the task; no drive-by cleanup, renames, or speculative abstractions.
- Do not modify already-merged SQLite migrations. This plan adds none.
- Do not hand-edit `backend/internal/storage/sqlite/gen/*`.
- API contracts are code-first: edit `backend/internal/httpd/controllers/dto.go` and `backend/internal/httpd/apispec/specgen/build.go`, then run `npm run api` from the repo root and commit `backend/internal/httpd/apispec/openapi.yaml` plus `frontend/src/api/schema.ts` with the Go change.
- Conventional commit messages (`feat:`, `fix:`, `docs:`, `test:`, `chore:`).
- All app state resolves under `~/.ao` (overridable via `AO_DATA_DIR`).
- The three `TestLifecycleDispatcherIsUnwired_*` tests in `internal/service/workboard/lifecycle_dispatcher_test.go` are **known-red by design** and unrelated to this plan. Every "run the full backend suite" step expects exactly those three to fail and nothing else.

## Decisions locked by this plan

The spec deliberately left four choices to planning. They are settled here — implementers must follow these, not re-litigate them:

1. **Bundle distribution:** embed the built JS in the Go binary and install it into `<dataDir>/director/` at boot, mirroring `internal/skillassets`. Keeps the daemon a single distributable binary and gives the adapter a stable absolute path, exactly the problem `skillassets` already solves. (Task 4.)
2. **Spawn path:** `Service.Spawn(ctx, ports.SpawnConfig{Kind: KindOrchestrator, Harness: <director harness>, …})` directly — **not** `SpawnOrchestrator`, which takes no harness parameter and resolves it from `Config.Orchestrator.Harness` (the wrong field). No `verifyOrchestratorReplacement` equivalent is added: that check enforces the "exactly one Hermes commander" invariant, which the Director path does not share. (Task 7.)
3. **`DisallowUnknownFields`:** not enabled in this plan. The DTO gap is fixed by declaring the missing fields (Task 6); turning on strict decoding could break callers passing extra keys and is a separate, wider decision.
4. **Default model:** `anthropic:claude-sonnet-4-6` when `agentConfig.model` is unset. (Task 2.)

**One deliberate divergence from the spec:** the spec's Error-handling section calls for a Director spawn failure to be recorded as a `dispatch_failed` work-card event with a new `director_unavailable` reason. This plan instead returns the error from `dispatchToDirector`, which the daemon logs. Reason: the `dispatch_failed` event is keyed to a specific card that the worker path had already claimed, whereas the Director path spawns a project-level commander with no card claimed yet — there is no card to attach the event to. Surfacing a per-card failure reason for the Director needs its own design and is deferred; it is listed under "Out of scope" at the end.

---

## File Structure

**New — `director/` TypeScript package (repo root, sibling to `backend/`/`frontend/`):**
- `director/package.json` — deps and scripts
- `director/tsconfig.json` — TS config
- `director/vitest.config.ts` — test config
- `director/src/config.ts` — reads env → typed config; resolves model/default
- `director/src/budget.ts` — iteration tracking + the forced-blocked rule
- `director/src/tools.ts` — `ao`-CLI-backed DeepAgents tools
- `director/src/prompt.ts` — Director system prompt
- `director/src/index.ts` — entrypoint wiring the above into a DeepAgents agent
- `director/src/*.test.ts` — colocated vitest tests

**New — Go:**
- `backend/internal/directorassets/directorassets.go` — embeds + installs the built bundle
- `backend/internal/adapters/agent/director/director.go` — the harness adapter
- `backend/internal/service/workboard/director_dispatch.go` — opt-in detection + dispatch

**Modified — Go:**
- `backend/internal/domain/harness.go` — add `HarnessDirector`
- `backend/internal/domain/projectconfig.go` — add `Director RoleOverride` + validation
- `backend/internal/adapters/agent/registry/registry.go` — register the adapter
- `backend/internal/session_manager/manager.go` — prompt env for the director harness
- `backend/internal/daemon/daemon.go` — install the bundle at boot
- `backend/internal/service/workboard/dispatch.go` — one additive branch
- `backend/internal/cli/project.go` — DTO gap fix + new flags

**Modified — frontend:**
- `frontend/src/renderer/components/WorkCardFocusPanel.tsx` — generalize the commander predicate

---

### Task 1: Scaffold the `director/` TypeScript package

**Files:**
- Create: `director/package.json`, `director/tsconfig.json`, `director/vitest.config.ts`, `director/.gitignore`, `director/src/index.ts`

**Interfaces:**
- Consumes: nothing (first task).
- Produces: a buildable package whose `npm run build` emits `director/dist/index.js`, and `npm test` runs vitest. Later tasks add modules under `director/src/`.

- [ ] **Step 1: Create the package manifest**

Create `director/package.json`:

```json
{
	"name": "@modern-agent/director",
	"private": true,
	"version": "0.0.1",
	"type": "module",
	"main": "dist/index.js",
	"scripts": {
		"build": "tsc -p tsconfig.json",
		"typecheck": "tsc -p tsconfig.json --noEmit",
		"test": "vitest run"
	},
	"dependencies": {
		"@langchain/core": "^0.3.0",
		"deepagents": "^0.1.0",
		"langchain": "^0.3.0"
	},
	"devDependencies": {
		"typescript": "^5.6.0",
		"vitest": "^2.1.0",
		"@types/node": "^22.0.0"
	}
}
```

Version ranges are starting points — Step 3 installs and records what actually resolves.

- [ ] **Step 2: Create the TypeScript and test config**

Create `director/tsconfig.json`:

```json
{
	"compilerOptions": {
		"target": "ES2022",
		"module": "ES2022",
		"moduleResolution": "bundler",
		"outDir": "dist",
		"rootDir": "src",
		"strict": true,
		"esModuleInterop": true,
		"skipLibCheck": true,
		"declaration": false,
		"types": ["node"]
	},
	"include": ["src/**/*.ts"],
	"exclude": ["src/**/*.test.ts"]
}
```

Create `director/vitest.config.ts`:

```ts
import { defineConfig } from "vitest/config";

export default defineConfig({
	test: {
		environment: "node",
		include: ["src/**/*.test.ts"],
	},
});
```

Create `director/.gitignore`:

```
node_modules/
dist/
```

- [ ] **Step 3: Install dependencies**

Run: `cd director && npm install`
Expected: succeeds, creates `director/node_modules/` and `director/package-lock.json`.

If a listed version range does not resolve, install the current published version instead (`npm install deepagents langchain @langchain/core`) and let npm write the real versions into `package.json`. Record in your report which versions resolved — the plan's ranges are a starting point, not a constraint.

- [ ] **Step 4: Create a placeholder entrypoint**

Create `director/src/index.ts`:

```ts
// Entrypoint for the AO Director agent. Wired up in a later task; this
// placeholder exists so the package builds from the start.
export {};
```

- [ ] **Step 5: Verify the package builds and tests run**

Run: `cd director && npm run build && npm test`
Expected: build succeeds and emits `director/dist/index.js`; vitest reports "No test files found" (exit 0) — that is expected with no tests yet. If vitest exits non-zero on no-tests in the installed version, add `"passWithNoTests": true` to the `test` block in `vitest.config.ts`.

- [ ] **Step 6: Commit**

```bash
git add director/package.json director/tsconfig.json director/vitest.config.ts director/.gitignore director/src/index.ts director/package-lock.json
git commit -m "feat(director): scaffold the Director TypeScript package"
```

---

### Task 2: Config module — engine resolution

**Files:**
- Create: `director/src/config.ts`
- Test: `director/src/config.test.ts`

**Interfaces:**
- Consumes: Task 1's package.
- Produces:
  - `export const DEFAULT_MODEL = "anthropic:claude-sonnet-4-6";`
  - `export interface DirectorConfig { model: string; cardId: string; sessionId: string; prompt: string; maxIterations: number; }`
  - `export function loadConfig(env: NodeJS.ProcessEnv): DirectorConfig` — throws `Error` on missing required values.

Environment contract (the Go adapter in Task 5 sets these): `AO_DIRECTOR_MODEL` (optional, defaults), `AO_DIRECTOR_CARD_ID` (required), `AO_SESSION_ID` (required), `AO_PROMPT` (optional), `AO_DIRECTOR_MAX_ITERATIONS` (optional, default 60).

- [ ] **Step 1: Write the failing test**

Create `director/src/config.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { DEFAULT_MODEL, loadConfig } from "./config.js";

const base = { AO_DIRECTOR_CARD_ID: "card-1", AO_SESSION_ID: "sess-1" };

describe("loadConfig", () => {
	it("uses the configured model", () => {
		const cfg = loadConfig({ ...base, AO_DIRECTOR_MODEL: "openrouter:minimax/minimax-m2" });
		expect(cfg.model).toBe("openrouter:minimax/minimax-m2");
	});

	it("falls back to the default model when unset", () => {
		expect(loadConfig(base).model).toBe(DEFAULT_MODEL);
	});

	it("falls back to the default model when blank", () => {
		expect(loadConfig({ ...base, AO_DIRECTOR_MODEL: "   " }).model).toBe(DEFAULT_MODEL);
	});

	it("throws when the card id is missing", () => {
		expect(() => loadConfig({ AO_SESSION_ID: "sess-1" })).toThrow(/AO_DIRECTOR_CARD_ID/);
	});

	it("throws when the session id is missing", () => {
		expect(() => loadConfig({ AO_DIRECTOR_CARD_ID: "card-1" })).toThrow(/AO_SESSION_ID/);
	});

	it("defaults maxIterations to 60", () => {
		expect(loadConfig(base).maxIterations).toBe(60);
	});

	it("reads maxIterations when set", () => {
		expect(loadConfig({ ...base, AO_DIRECTOR_MAX_ITERATIONS: "10" }).maxIterations).toBe(10);
	});

	it("rejects a non-numeric maxIterations rather than silently defaulting", () => {
		expect(() => loadConfig({ ...base, AO_DIRECTOR_MAX_ITERATIONS: "abc" })).toThrow(/AO_DIRECTOR_MAX_ITERATIONS/);
	});
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd director && npx vitest run src/config.test.ts`
Expected: FAIL — cannot resolve `./config.js`.

- [ ] **Step 3: Write the implementation**

Create `director/src/config.ts`:

```ts
/**
 * Engine default. Overridden per project via `agentConfig.model`, which the Go
 * adapter forwards as AO_DIRECTOR_MODEL. DeepAgents takes "provider:model-name",
 * so openai:, anthropic:, google:, openrouter:, fireworks:, baseten:, and
 * ollama: are all selectable.
 */
export const DEFAULT_MODEL = "anthropic:claude-sonnet-4-6";

const DEFAULT_MAX_ITERATIONS = 60;

export interface DirectorConfig {
	model: string;
	cardId: string;
	sessionId: string;
	prompt: string;
	maxIterations: number;
}

function required(env: NodeJS.ProcessEnv, key: string): string {
	const value = (env[key] ?? "").trim();
	if (value === "") {
		throw new Error(`${key} is required but was not set`);
	}
	return value;
}

export function loadConfig(env: NodeJS.ProcessEnv): DirectorConfig {
	const cardId = required(env, "AO_DIRECTOR_CARD_ID");
	const sessionId = required(env, "AO_SESSION_ID");

	const model = (env.AO_DIRECTOR_MODEL ?? "").trim() || DEFAULT_MODEL;

	// A malformed budget must fail loudly: silently falling back would hide the
	// misconfiguration behind a budget the operator did not choose.
	const rawMax = (env.AO_DIRECTOR_MAX_ITERATIONS ?? "").trim();
	let maxIterations = DEFAULT_MAX_ITERATIONS;
	if (rawMax !== "") {
		const parsed = Number(rawMax);
		if (!Number.isInteger(parsed) || parsed <= 0) {
			throw new Error(`AO_DIRECTOR_MAX_ITERATIONS must be a positive integer, got ${JSON.stringify(rawMax)}`);
		}
		maxIterations = parsed;
	}

	return { model, cardId, sessionId, prompt: (env.AO_PROMPT ?? "").trim(), maxIterations };
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd director && npx vitest run src/config.test.ts`
Expected: PASS — 8 tests.

- [ ] **Step 5: Commit**

```bash
git add director/src/config.ts director/src/config.test.ts
git commit -m "feat(director): resolve engine model and run config from env"
```

---

### Task 3: Budget module — the forced-blocked rule

This is the motivating fix: the Hermes commander exhausted its 60-iteration budget waiting on a human instead of recording a blocked card. Here that rule is code, not a prompt request.

**Files:**
- Create: `director/src/budget.ts`
- Test: `director/src/budget.test.ts`

**Interfaces:**
- Consumes: nothing from earlier tasks (pure module).
- Produces:
  - `export class IterationBudget { constructor(max: number); get used(): number; get remaining(): number; recordIteration(): void; shouldForceBlock(): boolean; }`
  - `export const RESERVE_ITERATIONS = 3;`

`shouldForceBlock()` returns true once `remaining <= RESERVE_ITERATIONS`, leaving room to actually issue the blocking transition before the budget is gone.

- [ ] **Step 1: Write the failing test**

Create `director/src/budget.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { IterationBudget, RESERVE_ITERATIONS } from "./budget.js";

describe("IterationBudget", () => {
	it("starts unused", () => {
		const b = new IterationBudget(10);
		expect(b.used).toBe(0);
		expect(b.remaining).toBe(10);
	});

	it("counts iterations", () => {
		const b = new IterationBudget(10);
		b.recordIteration();
		b.recordIteration();
		expect(b.used).toBe(2);
		expect(b.remaining).toBe(8);
	});

	it("does not force a block while budget is comfortable", () => {
		const b = new IterationBudget(10);
		b.recordIteration();
		expect(b.shouldForceBlock()).toBe(false);
	});

	it("forces a block once the reserve is reached, before the budget is spent", () => {
		const b = new IterationBudget(10);
		for (let i = 0; i < 10 - RESERVE_ITERATIONS; i++) b.recordIteration();
		expect(b.remaining).toBe(RESERVE_ITERATIONS);
		expect(b.shouldForceBlock()).toBe(true);
		// The whole point: there is still budget left to issue the transition.
		expect(b.remaining).toBeGreaterThan(0);
	});

	it("stays forced past exhaustion and never reports negative remaining", () => {
		const b = new IterationBudget(2);
		for (let i = 0; i < 5; i++) b.recordIteration();
		expect(b.shouldForceBlock()).toBe(true);
		expect(b.remaining).toBe(0);
	});

	it("rejects a non-positive max", () => {
		expect(() => new IterationBudget(0)).toThrow(/positive/);
	});
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd director && npx vitest run src/budget.test.ts`
Expected: FAIL — cannot resolve `./budget.js`.

- [ ] **Step 3: Write the implementation**

Create `director/src/budget.ts`:

```ts
/**
 * Iterations held back so the agent can still issue its blocking transition
 * after deciding to stop. Without this reserve the agent would notice it is out
 * of budget at the exact moment it no longer has budget to act — the failure
 * mode this module exists to prevent.
 */
export const RESERVE_ITERATIONS = 3;

export class IterationBudget {
	readonly #max: number;
	#used = 0;

	constructor(max: number) {
		if (!Number.isInteger(max) || max <= 0) {
			throw new Error(`iteration budget must be a positive integer, got ${max}`);
		}
		this.#max = max;
	}

	get used(): number {
		return this.#used;
	}

	get remaining(): number {
		return Math.max(0, this.#max - this.#used);
	}

	recordIteration(): void {
		this.#used++;
	}

	/**
	 * True once the agent must stop deliberating and block the card, while it
	 * still has enough budget left to record why.
	 */
	shouldForceBlock(): boolean {
		return this.remaining <= RESERVE_ITERATIONS;
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd director && npx vitest run src/budget.test.ts`
Expected: PASS — 6 tests.

- [ ] **Step 5: Commit**

```bash
git add director/src/budget.ts director/src/budget.test.ts
git commit -m "feat(director): enforce blocking the card before the budget runs out"
```

---

### Task 4: Tools module — `ao`-CLI-backed actions

**Files:**
- Create: `director/src/tools.ts`
- Test: `director/src/tools.test.ts`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `export type Runner = (argv: string[]) => Promise<{ code: number; stdout: string; stderr: string }>;`
  - `export function buildShowCardArgv(cardId: string): string[]`
  - `export function buildTransitionArgv(cardId: string, to: string, reason: string): string[]`
  - `export function buildSpawnWorkerArgv(agent: string, prompt: string): string[]`
  - `export async function runAo(run: Runner, argv: string[]): Promise<string>` — returns stdout, throws on non-zero exit.

The argv builders are pure and unit-testable; `Runner` is injected so tests never spawn a real process. Task 8 wires these into DeepAgents `tool()` objects.

`ao` resolves correctly because the session manager pins the daemon's own directory onto `PATH` (`HookPATH`, `manager.go`) — no absolute path needed.

- [ ] **Step 1: Write the failing test**

Create `director/src/tools.test.ts`:

```ts
import { describe, expect, it, vi } from "vitest";
import { buildShowCardArgv, buildSpawnWorkerArgv, buildTransitionArgv, runAo } from "./tools.js";

describe("argv builders", () => {
	it("builds the show-card argv with JSON output", () => {
		expect(buildShowCardArgv("card-1")).toEqual(["workboard", "card", "show", "card-1", "--json"]);
	});

	it("builds the transition argv", () => {
		expect(buildTransitionArgv("card-1", "review", "coding done")).toEqual([
			"workboard", "card", "transition", "card-1", "--to", "review", "--reason", "coding done",
		]);
	});

	it("builds the spawn-worker argv", () => {
		expect(buildSpawnWorkerArgv("claude-code", "implement X")).toEqual([
			"spawn", "--agent", "claude-code", "--prompt", "implement X",
		]);
	});

	it("rejects a transition with an empty reason", () => {
		expect(() => buildTransitionArgv("card-1", "blocked", "  ")).toThrow(/reason/);
	});
});

describe("runAo", () => {
	it("returns stdout on success", async () => {
		const run = vi.fn().mockResolvedValue({ code: 0, stdout: "ok", stderr: "" });
		await expect(runAo(run, ["workboard", "card", "show", "card-1"])).resolves.toBe("ok");
		expect(run).toHaveBeenCalledWith(["workboard", "card", "show", "card-1"]);
	});

	it("throws on a non-zero exit so the agent sees a tool error", async () => {
		const run = vi.fn().mockResolvedValue({ code: 1, stdout: "", stderr: "boom" });
		await expect(runAo(run, ["workboard", "card", "show", "card-1"])).rejects.toThrow(/boom/);
	});
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd director && npx vitest run src/tools.test.ts`
Expected: FAIL — cannot resolve `./tools.js`.

- [ ] **Step 3: Write the implementation**

Create `director/src/tools.ts`:

```ts
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

export function buildSpawnWorkerArgv(agent: string, prompt: string): string[] {
	return ["spawn", "--agent", agent, "--prompt", prompt];
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
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd director && npx vitest run src/tools.test.ts`
Expected: PASS — 6 tests.

- [ ] **Step 5: Commit**

```bash
git add director/src/tools.ts director/src/tools.test.ts
git commit -m "feat(director): add ao-CLI-backed tool argv builders and runner"
```

---

### Task 5: Prompt module

**Files:**
- Create: `director/src/prompt.ts`
- Test: `director/src/prompt.test.ts`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `export function directorSystemPrompt(cardId: string): string`

The prompt must state the current command names and the blocked-before-budget rule. The tests guard against two specific regressions: reintroducing the stale `ao workboard status` command that Hermes's prompt still references, and dropping the blocked instruction.

- [ ] **Step 1: Write the failing test**

Create `director/src/prompt.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { directorSystemPrompt } from "./prompt.js";

describe("directorSystemPrompt", () => {
	const prompt = directorSystemPrompt("card-42");

	it("names the card it owns", () => {
		expect(prompt).toContain("card-42");
	});

	it("uses the current transition command", () => {
		expect(prompt).toContain("ao workboard card transition");
	});

	it("does not reference the stale status command", () => {
		expect(prompt).not.toContain("ao workboard status");
	});

	it("instructs blocking before the budget is exhausted", () => {
		expect(prompt).toMatch(/blocked/i);
		expect(prompt).toMatch(/budget/i);
	});

	it("tells the agent to delegate implementation rather than code directly", () => {
		expect(prompt).toContain("ao spawn");
	});
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd director && npx vitest run src/prompt.test.ts`
Expected: FAIL — cannot resolve `./prompt.js`.

- [ ] **Step 3: Write the implementation**

Create `director/src/prompt.ts`:

```ts
export function directorSystemPrompt(cardId: string): string {
	return `You are the Director for AO work card ${cardId}. You own this card until it reaches a terminal state (done or blocked).

## How you work

Read the card first: \`ao workboard card show ${cardId} --json\`. Use the live card, never a stale copy of it.

Plan the work, then delegate implementation to worker sessions with \`ao spawn --agent <agent> --prompt "<task>"\`. Include the card id and the exact subtask in every worker prompt. Keep at most one implementation worker active at a time. Do not write the implementation yourself except for a small coordination-only fix.

Advance the card with \`ao workboard card transition ${cardId} --to <status> --reason "<why>"\` once a phase genuinely completes. Valid onward statuses are review, testing, done, redo, and blocked. The daemon validates every transition; an invalid one is rejected and you must read the error rather than retrying blindly.

## When you cannot proceed

If the work is ambiguous, a check cannot run, a worker fails, or you need a human decision — block the card immediately:

\`ao workboard card transition ${cardId} --to blocked --reason "<the exact question or obstacle>"\`

Do this **as soon as you identify the blocker**, not after deliberating about it. Your iteration budget is finite. A blocked card carrying a precise question is a good outcome; a session that exhausts its budget while thinking about the question leaves the card stuck and tells the human nothing. Never end a run without the card in a terminal state or a recorded blocked reason.`;
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd director && npx vitest run src/prompt.test.ts`
Expected: PASS — 5 tests.

- [ ] **Step 5: Commit**

```bash
git add director/src/prompt.ts director/src/prompt.test.ts
git commit -m "feat(director): add the Director system prompt"
```

---

### Task 6: Entrypoint — wire the DeepAgents loop

**Files:**
- Modify: `director/src/index.ts`

**Interfaces:**
- Consumes: `loadConfig`/`DirectorConfig` (Task 2), `IterationBudget` (Task 3), the tool builders + `Runner`/`runAo` (Task 4), `directorSystemPrompt` (Task 5).
- Produces: a runnable `director/dist/index.js` that the Go adapter launches.

This task has no new unit tests: it is composition over modules already covered, and its real verification is the end-to-end run in Task 12. Keep logic here minimal — anything worth testing belongs in one of the modules above.

- [ ] **Step 1: Write the entrypoint**

Replace `director/src/index.ts` entirely:

```ts
import { spawn } from "node:child_process";
import { createDeepAgent } from "deepagents";
import { tool } from "langchain";
import { z } from "zod";
import { IterationBudget } from "./budget.js";
import { loadConfig } from "./config.js";
import { directorSystemPrompt } from "./prompt.js";
import { buildShowCardArgv, buildSpawnWorkerArgv, buildTransitionArgv, runAo, type Runner } from "./tools.js";

/** Runs `ao` from PATH. The session manager pins the daemon's own directory
 * onto PATH, so a bare `ao` resolves to the daemon that spawned this session. */
const runner: Runner = (argv) =>
	new Promise((resolve, reject) => {
		const child = spawn("ao", argv, { stdio: ["ignore", "pipe", "pipe"] });
		let stdout = "";
		let stderr = "";
		child.stdout.on("data", (c) => { stdout += String(c); });
		child.stderr.on("data", (c) => { stderr += String(c); });
		child.on("error", reject);
		child.on("close", (code) => resolve({ code: code ?? 1, stdout, stderr }));
	});

async function main(): Promise<void> {
	const cfg = loadConfig(process.env);
	const budget = new IterationBudget(cfg.maxIterations);

	const showCard = tool(
		async () => runAo(runner, buildShowCardArgv(cfg.cardId)),
		{
			name: "show_card",
			description: "Read the current state of the work card you own, as JSON.",
			schema: z.object({}),
		},
	);

	const transitionCard = tool(
		async ({ to, reason }: { to: string; reason: string }) =>
			runAo(runner, buildTransitionArgv(cfg.cardId, to, reason)),
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
			runAo(runner, buildSpawnWorkerArgv(agent, prompt)),
		{
			name: "spawn_worker",
			description: "Delegate an implementation subtask to a worker agent session.",
			schema: z.object({
				agent: z.string().describe("Harness to run the worker on, e.g. claude-code"),
				prompt: z.string().describe("The exact subtask, including the card id"),
			}),
		},
	);

	const agent = await createDeepAgent({
		model: cfg.model,
		tools: [showCard, transitionCard, spawnWorker],
		systemPrompt: directorSystemPrompt(cfg.cardId),
	});

	const opening = cfg.prompt.trim() || `Drive work card ${cfg.cardId} to completion.`;
	let messages: Array<{ role: string; content: string }> = [{ role: "user", content: opening }];

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
			return;
		}
		budget.recordIteration();
		const result = await agent.invoke({ messages });
		messages = result.messages as typeof messages;
		if (isFinished(result)) return;
	}
}

/** DeepAgents returns when the model stops requesting tools; treat the absence
 * of a pending tool call as the run being complete. */
function isFinished(result: unknown): boolean {
	const messages = (result as { messages?: Array<{ tool_calls?: unknown[] }> }).messages ?? [];
	const last = messages[messages.length - 1];
	return !last?.tool_calls || last.tool_calls.length === 0;
}

function summarize(messages: Array<{ role: string; content: string }>): string {
	const last = messages[messages.length - 1];
	return (last?.content ?? "").slice(0, 500);
}

main().catch((err: unknown) => {
	console.error(`director: ${err instanceof Error ? err.message : String(err)}`);
	process.exitCode = 1;
});
```

- [ ] **Step 2: Add the zod dependency**

`zod` is used for the tool schemas. Run: `cd director && npm install zod`
Expected: succeeds and adds `zod` to `dependencies`.

- [ ] **Step 3: Verify the package builds and all tests still pass**

Run: `cd director && npm run build && npm test`
Expected: build emits `director/dist/index.js`; all tests from Tasks 2-5 pass.

**If the DeepAgents or LangChain API differs from what this step assumes** — `createDeepAgent`'s options, the `tool()` signature, or the shape of `agent.invoke`'s result — consult the installed package's own types (`director/node_modules/deepagents`, `node_modules/langchain`) and the docs at https://docs.langchain.com/oss/javascript/deepagents/overview, and adapt. The plan's contract is the behavior (a loop that enforces the budget/blocked rule and exposes those three tools), not these exact call signatures. Note any deviation in your report.

- [ ] **Step 4: Commit**

```bash
git add director/src/index.ts director/package.json director/package-lock.json
git commit -m "feat(director): wire the DeepAgents loop with ao-backed tools"
```

---

### Task 7: Embed and install the Director bundle

Mirrors `internal/skillassets` (read it first — this task is deliberately the same shape).

**Files:**
- Create: `backend/internal/directorassets/directorassets.go`
- Test: `backend/internal/directorassets/directorassets_test.go`
- Modify: `backend/internal/daemon/daemon.go`

**Interfaces:**
- Consumes: `director/dist/index.js` (Task 6's build output).
- Produces:
  - `directorassets.Dir(dataDir string) string` → `<dataDir>/director`
  - `directorassets.EntrypointPath(dataDir string) string` → `<dataDir>/director/index.js`
  - `directorassets.Install(dataDir string) error`

Go's `embed` cannot reach outside its own module directory, so the built bundle must be copied into the Go package before embedding.

- [ ] **Step 1: Vendor the built bundle into the Go package**

```bash
mkdir -p backend/internal/directorassets/bundle
cp director/dist/index.js backend/internal/directorassets/bundle/index.js
```

Note in your report that this copy is a build artifact committed into the repo, and that rebuilding the director package requires re-running this copy. (A future task may automate it via an npm script; wiring that is out of scope here.)

- [ ] **Step 2: Write the failing test**

Create `backend/internal/directorassets/directorassets_test.go`:

```go
package directorassets_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/modernagent/modern-agent/backend/internal/directorassets"
)

func TestInstallWritesEntrypoint(t *testing.T) {
	dir := t.TempDir()
	if err := directorassets.Install(dir); err != nil {
		t.Fatalf("Install: %v", err)
	}
	path := directorassets.EntrypointPath(dir)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat entrypoint: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("installed entrypoint is empty")
	}
	if want := filepath.Join(dir, "director", "index.js"); path != want {
		t.Fatalf("EntrypointPath = %q, want %q", path, want)
	}
}

func TestInstallIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 2; i++ {
		if err := directorassets.Install(dir); err != nil {
			t.Fatalf("Install (run %d): %v", i, err)
		}
	}
	if _, err := os.Stat(directorassets.EntrypointPath(dir)); err != nil {
		t.Fatalf("stat after reinstall: %v", err)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd backend && go test ./internal/directorassets/`
Expected: FAIL to build — package does not exist.

- [ ] **Step 4: Write the implementation**

Create `backend/internal/directorassets/directorassets.go`:

```go
// Package directorassets embeds the built Director agent bundle and installs it
// into the AO data dir at daemon boot. It mirrors internal/skillassets: sessions
// run in a worktree of whatever project spawned them, so only an absolute path
// under the data dir resolves reliably. The embedded copy is the single source
// of truth and Install clobbers the on-disk copy every boot, so a new daemon
// build always refreshes it and the two cannot drift.
package directorassets

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed bundle/index.js
var files embed.FS

// DirName is the installed bundle's directory name under <dataDir>.
const DirName = "director"

// Dir returns the absolute directory the bundle installs into.
func Dir(dataDir string) string {
	return filepath.Join(dataDir, DirName)
}

// EntrypointPath returns the absolute path to the Director entrypoint the
// adapter launches with node.
func EntrypointPath(dataDir string) string {
	return filepath.Join(Dir(dataDir), "index.js")
}

// Install writes the embedded bundle into <dataDir>/director, replacing any
// existing copy. It runs at daemon boot before any session spawns.
func Install(dataDir string) error {
	dest := Dir(dataDir)
	if err := os.MkdirAll(dest, 0o750); err != nil {
		return fmt.Errorf("create director dir %q: %w", dest, err)
	}
	b, err := files.ReadFile("bundle/index.js")
	if err != nil {
		return fmt.Errorf("read embedded director bundle: %w", err)
	}
	target := EntrypointPath(dataDir)
	if err := os.WriteFile(target, b, 0o600); err != nil {
		return fmt.Errorf("write %q: %w", target, err)
	}
	return nil
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `cd backend && go test ./internal/directorassets/ -v`
Expected: PASS — both tests.

- [ ] **Step 6: Install the bundle at daemon boot**

In `backend/internal/daemon/daemon.go`, find the existing `skillassets.Install` block:

```go
	if err := skillassets.Install(cfg.DataDir); err != nil {
		log.Warn("install using-ao skill", "err", err)
	}
```

Add immediately after it:

```go
	// Same rationale as the skill above: sessions need a stable absolute path to
	// the Director bundle, and a failure is non-fatal — only projects that opt
	// into the Director harness need it.
	if err := directorassets.Install(cfg.DataDir); err != nil {
		log.Warn("install director bundle", "err", err)
	}
```

Add the import `"github.com/modernagent/modern-agent/backend/internal/directorassets"` to `daemon.go`'s import block.

- [ ] **Step 7: Verify the daemon builds and its tests pass**

Run: `cd backend && go build ./... && go test ./internal/daemon/... ./internal/directorassets/`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/directorassets/ backend/internal/daemon/daemon.go
git commit -m "feat(daemon): embed and install the Director agent bundle"
```

---

### Task 8: The `director` harness adapter

**Files:**
- Create: `backend/internal/adapters/agent/director/director.go`
- Test: `backend/internal/adapters/agent/director/director_test.go`
- Modify: `backend/internal/domain/harness.go`, `backend/internal/adapters/agent/registry/registry.go`, `backend/internal/session_manager/manager.go`

**Interfaces:**
- Consumes: `directorassets.EntrypointPath` (Task 7).
- Produces:
  - `domain.HarnessDirector AgentHarness = "director"` (in `AllHarnesses`)
  - `director.New() *director.Plugin` implementing `adapters.Adapter` + `ports.Agent`
  - Launch argv: `["node", "<dataDir>/director/index.js"]`
  - Env contract consumed by Task 2's `loadConfig`: `AO_DIRECTOR_MODEL`, `AO_DIRECTOR_CARD_ID`, `AO_PROMPT`

Read `backend/internal/adapters/agent/command/command.go` first — it is the closest template (launches a process, delivers the prompt via env rather than argv).

- [ ] **Step 1: Add the harness constant**

In `backend/internal/domain/harness.go`, add to the const block (next to `HarnessCommand`):

```go
	// HarnessDirector runs AO's first-party Director agent: a DeepAgents loop
	// that drives a work card through its phases. Unlike the CLI-wrapping
	// harnesses, AO owns its loop, so agentConfig.model selects its engine
	// ("provider:model-name", e.g. openrouter:minimax/minimax-m2).
	HarnessDirector AgentHarness = "director"
```

And add `HarnessDirector` to the `AllHarnesses` slice.

- [ ] **Step 2: Write the failing test**

Create `backend/internal/adapters/agent/director/director_test.go`:

```go
package director_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modernagent/modern-agent/backend/internal/adapters/agent/director"
	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/ports"
)

func TestManifestID(t *testing.T) {
	if got := director.New().Manifest().ID; got != string(domain.HarnessDirector) {
		t.Fatalf("manifest id = %q, want %q", got, domain.HarnessDirector)
	}
}

func TestGetLaunchCommandRunsTheInstalledBundle(t *testing.T) {
	p := director.New(director.WithDataDir("/data"))
	argv, err := p.GetLaunchCommand(context.Background(), ports.LaunchConfig{SessionID: "sess-1"})
	if err != nil {
		t.Fatalf("GetLaunchCommand: %v", err)
	}
	if len(argv) != 2 || argv[0] != "node" {
		t.Fatalf("argv = %v, want [node <path>]", argv)
	}
	if want := filepath.Join("/data", "director", "index.js"); argv[1] != want {
		t.Fatalf("entrypoint = %q, want %q", argv[1], want)
	}
}

func TestGetLaunchCommandRequiresDataDir(t *testing.T) {
	_, err := director.New().GetLaunchCommand(context.Background(), ports.LaunchConfig{SessionID: "sess-1"})
	if err == nil {
		t.Fatal("GetLaunchCommand: want error when the data dir is unset, got nil")
	}
	if !strings.Contains(err.Error(), "data dir") {
		t.Fatalf("error = %v, want it to name the missing data dir", err)
	}
}

func TestPromptDeliveryIsEnvBased(t *testing.T) {
	got, err := director.New().GetPromptDeliveryStrategy(context.Background(), ports.LaunchConfig{})
	if err != nil {
		t.Fatalf("GetPromptDeliveryStrategy: %v", err)
	}
	if got != ports.PromptDeliveryAfterStart {
		t.Fatalf("strategy = %v, want %v", got, ports.PromptDeliveryAfterStart)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd backend && go test ./internal/adapters/agent/director/`
Expected: FAIL to build — package does not exist.

- [ ] **Step 4: Write the adapter**

Create `backend/internal/adapters/agent/director/director.go`:

```go
// Package director implements AO's first-party Director agent harness. Unlike
// the other adapters, which wrap an externally installed CLI, the Director is a
// DeepAgents loop AO ships itself (see internal/directorassets) — which is what
// makes agentConfig.model a real engine selector rather than an advisory hint.
package director

import (
	"context"
	"errors"
	"fmt"

	"github.com/modernagent/modern-agent/backend/internal/adapters"
	"github.com/modernagent/modern-agent/backend/internal/directorassets"
	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/ports"
)

const adapterID = "director"

// Plugin is the Director harness adapter.
type Plugin struct {
	dataDir string
}

// Option configures a Plugin.
type Option func(*Plugin)

// WithDataDir sets the AO data dir the installed Director bundle lives under.
func WithDataDir(dir string) Option {
	return func(p *Plugin) { p.dataDir = dir }
}

// New returns a ready-to-register Director adapter.
func New(opts ...Option) *Plugin {
	p := &Plugin{}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

var _ adapters.Adapter = (*Plugin)(nil)
var _ ports.Agent = (*Plugin)(nil)
var _ ports.AgentAuthChecker = (*Plugin)(nil)

// Manifest returns the adapter's static self-description.
func (p *Plugin) Manifest() adapters.Manifest {
	return adapters.Manifest{
		ID:          adapterID,
		Name:        "Director",
		Description: "Drive work cards with AO's first-party Director agent on a configurable engine.",
		Version:     "0.0.1",
		Capabilities: []adapters.Capability{
			adapters.CapabilityAgent,
		},
	}
}

// GetConfigSpec reports the engine field exposed in project config.
func (p *Plugin) GetConfigSpec(ctx context.Context) (ports.ConfigSpec, error) {
	if err := ctx.Err(); err != nil {
		return ports.ConfigSpec{}, err
	}
	return ports.ConfigSpec{
		Fields: []ports.ConfigField{
			{
				Key:         "model",
				Type:        ports.ConfigFieldString,
				Description: `Engine as "provider:model-name" (e.g. openai:gpt-5, openrouter:minimax/minimax-m2, ollama:qwen2.5).`,
				Required:    false,
			},
		},
	}, nil
}

// GetLaunchCommand runs the installed Director bundle with node.
func (p *Plugin) GetLaunchCommand(ctx context.Context, cfg ports.LaunchConfig) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.dataDir == "" {
		return nil, errors.New("director harness requires the AO data dir to locate its bundle")
	}
	return []string{"node", directorassets.EntrypointPath(p.dataDir)}, nil
}

// GetPromptDeliveryStrategy reports that prompts arrive via env vars.
func (p *Plugin) GetPromptDeliveryStrategy(ctx context.Context, _ ports.LaunchConfig) (ports.PromptDeliveryStrategy, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return ports.PromptDeliveryAfterStart, nil
}

// GetAgentHooks is a no-op: the Director reports activity through `ao hooks`.
func (p *Plugin) GetAgentHooks(context.Context, ports.WorkspaceHookConfig) error {
	return nil
}

// GetRestoreCommand reports that Director sessions are not natively resumable
// yet: the loop rebuilds its context from the card on each run, so a fresh
// launch is equivalent to a resume.
func (p *Plugin) GetRestoreCommand(ctx context.Context, _ ports.RestoreConfig) ([]string, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	return nil, false, nil
}

// SessionInfo reports no agent-owned metadata.
func (p *Plugin) SessionInfo(ctx context.Context, _ ports.SessionRef) (ports.SessionInfo, bool, error) {
	if err := ctx.Err(); err != nil {
		return ports.SessionInfo{}, false, err
	}
	return ports.SessionInfo{}, false, nil
}

// AuthStatus reports authorized: the Director's provider key is validated by the
// agent itself at startup, where the configured provider is known.
func (p *Plugin) AuthStatus(ctx context.Context) (ports.AgentAuthStatus, error) {
	if err := ctx.Err(); err != nil {
		return ports.AgentAuthStatusUnknown, err
	}
	return ports.AgentAuthStatusAuthorized, nil
}
```

Drop the `fmt` import if `go build` reports it unused — the code above uses only `errors`.

- [ ] **Step 5: Run the adapter tests**

Run: `cd backend && go test ./internal/adapters/agent/director/ -v`
Expected: PASS — 4 tests.

- [ ] **Step 6: Register the adapter**

In `backend/internal/adapters/agent/registry/registry.go`, add the import
`"github.com/modernagent/modern-agent/backend/internal/adapters/agent/director"` and add `director.New(),` to the `Constructors()` slice.

**Check how the registry is constructed before wiring the data dir:** `New()` here takes no data dir, so the launch command would fail. Determine how `Constructors()` is called (grep for `registry.Constructors(` and `registry.Build(`) and thread the data dir through — either by giving `Constructors` a data-dir parameter, or by having the registry builder pass `director.WithDataDir(dataDir)`. Pick whichever fits the existing call sites with the smallest change, and state which you chose in your report.

- [ ] **Step 7: Deliver the prompt and engine env to the Director**

In `backend/internal/session_manager/manager.go`, `runtimeEnv` currently sets the prompt env vars only for the command harness:

```go
	if harness == domain.HarnessCommand {
		if prompt != "" {
			env[EnvPrompt] = prompt
		}
		if systemPrompt != "" {
			env[EnvSystemPrompt] = systemPrompt
		}
	}
```

Change the condition to cover the Director too, and add its engine variable:

```go
	if harness == domain.HarnessCommand || harness == domain.HarnessDirector {
		if prompt != "" {
			env[EnvPrompt] = prompt
		}
		if systemPrompt != "" {
			env[EnvSystemPrompt] = systemPrompt
		}
	}
```

The Director also needs `AO_DIRECTOR_MODEL` (from `agentConfig.model`) and `AO_DIRECTOR_CARD_ID`. `runtimeEnv`'s current signature does not carry the agent config or the card id. Read the two call sites (`manager.go:356` and `manager.go:783`) and thread through what is available; if the card id is not reachable there, set only `AO_DIRECTOR_MODEL` in this task and note in your report that `AO_DIRECTOR_CARD_ID` must be supplied by the dispatch path (Task 10) — that is an acceptable split, since the dispatcher is what knows the card.

- [ ] **Step 8: Verify the whole backend builds and tests pass**

Run: `cd backend && go build ./... && go test ./...`
Expected: PASS except the three known-red `TestLifecycleDispatcherIsUnwired_*` tests.

- [ ] **Step 9: Commit**

```bash
git add backend/internal/domain/harness.go backend/internal/adapters/agent/director/ backend/internal/adapters/agent/registry/registry.go backend/internal/session_manager/manager.go
git commit -m "feat(adapters): register the Director agent harness"
```

---

### Task 9: `Config.Director` in the domain

**Files:**
- Modify: `backend/internal/domain/projectconfig.go`
- Test: `backend/internal/domain/projectconfig_test.go`

**Interfaces:**
- Consumes: `domain.HarnessDirector` (Task 8).
- Produces: `ProjectConfig.Director RoleOverride` with JSON tag `director`, validated like `Worker`/`Orchestrator`.

- [ ] **Step 1: Write the failing test**

Add to `backend/internal/domain/projectconfig_test.go`:

```go
func TestProjectConfigDirectorValidation(t *testing.T) {
	valid := ProjectConfig{Director: RoleOverride{Harness: HarnessDirector}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate with a known director harness: %v", err)
	}

	invalid := ProjectConfig{Director: RoleOverride{Harness: AgentHarness("nope")}}
	err := invalid.Validate()
	if err == nil {
		t.Fatal("Validate: want error for an unknown director harness, got nil")
	}
	if !strings.Contains(err.Error(), "director") {
		t.Fatalf("error = %v, want it to name the director role", err)
	}
}
```

Check `projectconfig_test.go`'s existing imports and validation-method name before writing — if the method is not called `Validate()`, use the real name (grep for the function the existing role-override validation lives in, around `projectconfig.go:312-319`). Add `"strings"` to the imports if absent.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/domain/ -run TestProjectConfigDirectorValidation`
Expected: FAIL to build — `ProjectConfig` has no field `Director`.

- [ ] **Step 3: Add the field and validation**

In `backend/internal/domain/projectconfig.go`, add to the `ProjectConfig` struct next to `Orchestrator`:

```go
	// Director opts the project into AO's first-party Director agent, which
	// drives work cards itself. Unset means the project uses the existing
	// dispatch paths unchanged.
	Director RoleOverride `json:"director,omitempty"`
```

In the validation loop (around line 312), add `"director": c.Director` to the role map so an unknown harness is rejected with the role named.

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd backend && go test ./internal/domain/ -v`
Expected: PASS — the new test plus every existing domain test.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/domain/projectconfig.go backend/internal/domain/projectconfig_test.go
git commit -m "feat(domain): add the Director role to project config"
```

---

### Task 10: Director dispatch

**Files:**
- Create: `backend/internal/service/workboard/director_dispatch.go`
- Test: `backend/internal/service/workboard/director_dispatch_test.go`
- Modify: `backend/internal/service/workboard/dispatch.go`

**Interfaces:**
- Consumes: `ProjectConfig.Director` (Task 9), `domain.HarnessDirector` (Task 8), the existing `DispatchStore`, and `WorkerSpawner` (`Spawn(ctx, ports.SpawnConfig) (domain.Session, error)`) already declared in `dispatch.go`.
- Produces:
  - `func directorEnabled(project domain.ProjectRecord) bool`
  - `func isProjectDirector(session domain.SessionRecord, harness domain.AgentHarness) bool`
  - `func (d *Dispatcher) dispatchToDirector(ctx context.Context, project domain.ProjectRecord, cards []domain.WorkCard, now time.Time) ([]string, error)`

Per the locked decisions: spawn via `Service.Spawn` with an explicit `Harness`, **not** `SpawnOrchestrator`. No `verifyOrchestratorReplacement` equivalent.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/service/workboard/director_dispatch_test.go`. Reuse this package's existing dispatch fakes (`newDispatchStore`, `dispatchSpawner`, `todoCard`) — read `dispatch_test.go` first and match its construction exactly rather than adding new fakes:

```go
package workboard

import (
	"context"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

func TestDirectorEnabled(t *testing.T) {
	off := domain.ProjectRecord{Config: &domain.ProjectConfig{}}
	if directorEnabled(off) {
		t.Fatal("directorEnabled = true for a project with no director configured")
	}
	on := domain.ProjectRecord{Config: &domain.ProjectConfig{
		Director: domain.RoleOverride{Harness: domain.HarnessDirector},
	}}
	if !directorEnabled(on) {
		t.Fatal("directorEnabled = false for a project with a director configured")
	}
}

func TestIsProjectDirector(t *testing.T) {
	live := domain.SessionRecord{Kind: domain.KindOrchestrator, Harness: domain.HarnessDirector}
	if !isProjectDirector(live, domain.HarnessDirector) {
		t.Fatal("a live orchestrator session on the director harness should count")
	}
	terminated := live
	terminated.IsTerminated = true
	if isProjectDirector(terminated, domain.HarnessDirector) {
		t.Fatal("a terminated session should not count")
	}
	worker := domain.SessionRecord{Kind: domain.KindWorker, Harness: domain.HarnessDirector}
	if isProjectDirector(worker, domain.HarnessDirector) {
		t.Fatal("a worker session should not count")
	}
	other := domain.SessionRecord{Kind: domain.KindOrchestrator, Harness: domain.HarnessClaudeCode}
	if isProjectDirector(other, domain.HarnessDirector) {
		t.Fatal("a different harness should not count")
	}
}

func TestDispatchOnceSpawnsDirectorForOptedInProject(t *testing.T) {
	now := time.Date(2026, time.August, 3, 9, 0, 0, 0, time.UTC)
	store := newDispatchStore(4, []domain.WorkCard{todoCard("c1", domain.CardPriorityNormal, now)})
	store.projectConfig = &domain.ProjectConfig{Director: domain.RoleOverride{Harness: domain.HarnessDirector}}
	spawner := &dispatchSpawner{}
	dispatcher := NewDispatcher(DispatchDeps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

	if _, err := dispatcher.DispatchOnce(context.Background(), "p1"); err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
	if len(spawner.configs) != 1 {
		t.Fatalf("spawn calls = %d, want 1", len(spawner.configs))
	}
	got := spawner.configs[0]
	if got.Harness != domain.HarnessDirector {
		t.Fatalf("Harness = %q, want %q", got.Harness, domain.HarnessDirector)
	}
	if got.Kind != domain.KindOrchestrator {
		t.Fatalf("Kind = %q, want %q", got.Kind, domain.KindOrchestrator)
	}
}

func TestDispatchOnceReusesTheLiveDirectorSession(t *testing.T) {
	now := time.Date(2026, time.August, 3, 9, 0, 0, 0, time.UTC)
	store := newDispatchStore(4, []domain.WorkCard{todoCard("c1", domain.CardPriorityNormal, now)})
	store.projectConfig = &domain.ProjectConfig{Director: domain.RoleOverride{Harness: domain.HarnessDirector}}
	store.sessions = []domain.SessionRecord{{
		ID: "director-1", ProjectID: "p1", Kind: domain.KindOrchestrator, Harness: domain.HarnessDirector,
	}}
	spawner := &dispatchSpawner{}
	dispatcher := NewDispatcher(DispatchDeps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

	if _, err := dispatcher.DispatchOnce(context.Background(), "p1"); err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
	if len(spawner.configs) != 0 {
		t.Fatalf("spawn calls = %d, want 0 (the live director should be reused)", len(spawner.configs))
	}
}

func TestDispatchOnceLeavesNonDirectorProjectsUnchanged(t *testing.T) {
	now := time.Date(2026, time.August, 3, 9, 0, 0, 0, time.UTC)
	store := newDispatchStore(4, []domain.WorkCard{todoCard("c1", domain.CardPriorityNormal, now)})
	spawner := &dispatchSpawner{}
	dispatcher := NewDispatcher(DispatchDeps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

	claimed, err := dispatcher.DispatchOnce(context.Background(), "p1")
	if err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed = %v, want the existing worker path to claim the card", claimed)
	}
	if len(spawner.configs) != 1 || spawner.configs[0].Kind != domain.KindWorker {
		t.Fatalf("configs = %+v, want one worker spawn from the unchanged path", spawner.configs)
	}
}
```

The fake store may need a `projectConfig` field and a `sessions` field if it does not already expose them — check `dispatch_test.go`'s `dispatchStore` first and extend it minimally (matching how `GetProject`/`ListSessions` already return data) rather than creating a parallel fake.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/service/workboard/ -run 'TestDirector|TestIsProjectDirector|TestDispatchOnceSpawnsDirector|TestDispatchOnceReusesTheLiveDirector'`
Expected: FAIL to build — `directorEnabled`, `isProjectDirector` undefined.

- [ ] **Step 3: Write the dispatch module**

Create `backend/internal/service/workboard/director_dispatch.go`:

```go
package workboard

import (
	"context"
	"fmt"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/ports"
)

// directorEnabled reports whether the project opted into AO's Director agent.
func directorEnabled(project domain.ProjectRecord) bool {
	return project.Config != nil && project.Config.Director.Harness != ""
}

// isProjectDirector reports whether a session is this project's live Director.
// Deliberately independent of isHermesCommander: the two commander mechanisms
// share no state and must not share a predicate.
func isProjectDirector(session domain.SessionRecord, harness domain.AgentHarness) bool {
	return !session.IsTerminated && session.Kind == domain.KindOrchestrator && session.Harness == harness
}

// dispatchToDirector ensures the project has one live Director session. The
// Director drives cards itself through `ao workboard card transition`, so this
// path does not claim cards the way the worker path does — it only guarantees
// the commander is running.
func (d *Dispatcher) dispatchToDirector(ctx context.Context, project domain.ProjectRecord, _ []domain.WorkCard, _ time.Time) ([]string, error) {
	harness := project.Config.Director.Harness

	sessions, err := d.store.ListSessions(ctx, domain.ProjectID(project.ID))
	if err != nil {
		return nil, fmt.Errorf("list sessions for project %s: %w", project.ID, err)
	}
	for _, session := range sessions {
		if isProjectDirector(session, harness) {
			return nil, nil // already running
		}
	}

	// Spawn directly rather than through SpawnOrchestrator: that helper takes no
	// harness and resolves it from Config.Orchestrator.Harness, which is the
	// wrong field for the Director.
	if _, err := d.spawner.Spawn(ctx, ports.SpawnConfig{
		ProjectID: domain.ProjectID(project.ID),
		Kind:      domain.KindOrchestrator,
		Harness:   harness,
		Prompt:    fmt.Sprintf("Drive the work cards for project %s.", project.ID),
	}); err != nil {
		return nil, fmt.Errorf("spawn director for project %s: %w", project.ID, err)
	}
	return nil, nil
}
```

- [ ] **Step 4: Add the dispatch branch**

In `backend/internal/service/workboard/dispatch.go`, in `DispatchOnce`, immediately before the existing line `commanding := project.Config.Orchestrator.Harness == domain.HarnessHermes`, add:

```go
	if directorEnabled(project) {
		return d.dispatchToDirector(ctx, project, cards, d.clock().UTC())
	}
```

**Check the surrounding code before inserting:** `cards` must already be loaded at that point. If `ListWorkCards` happens after the `commanding` line, move the branch to just after the cards are loaded, or pass `nil` for cards (the function ignores them). Do not reorder any existing statement to accommodate it.

Nothing else in `dispatch.go` changes.

- [ ] **Step 5: Run the workboard package tests**

Run: `cd backend && go test ./internal/service/workboard/ -v`
Expected: PASS for every new test and every pre-existing dispatch test, except the three known-red `TestLifecycleDispatcherIsUnwired_*`.

- [ ] **Step 6: Verify the full backend**

Run: `cd backend && go build ./... && go test ./...`
Expected: PASS except the three known-red tests.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/service/workboard/director_dispatch.go backend/internal/service/workboard/director_dispatch_test.go backend/internal/service/workboard/dispatch.go
git commit -m "feat(workboard): dispatch work through the Director agent when configured"
```

---

### Task 11: CLI config plumbing

Two things: fix the pre-existing DTO gap that silently drops config fields, and add first-class Director flags.

**Files:**
- Modify: `backend/internal/cli/project.go`
- Test: `backend/internal/cli/project_test.go`

**Interfaces:**
- Consumes: `Config.Director` (Task 9).
- Produces: `--director-agent`, `--director-model` flags on `ao project set-config`; `projectConfig`/`agentConfig` CLI DTOs that no longer drop `command` or `director`.

- [ ] **Step 1: Write the failing test**

Add to `backend/internal/cli/project_test.go` (match the file's existing test style and helpers):

```go
func TestBuildProjectConfigJSONKeepsCommandAndDirector(t *testing.T) {
	opts := projectSetConfigOptions{configJSON: `{
		"worker": {"agentConfig": {"command": ["/bin/sh", "-c", "echo hi"]}},
		"director": {"agent": "director", "agentConfig": {"model": "openrouter:minimax/minimax-m2"}}
	}`}
	cfg, err := buildProjectConfig(opts)
	if err != nil {
		t.Fatalf("buildProjectConfig: %v", err)
	}
	if got := cfg.Worker.AgentConfig.Command; len(got) != 3 || got[0] != "/bin/sh" {
		t.Fatalf("worker command = %v, want the full argv (this is the silent-drop bug)", got)
	}
	if cfg.Director.Agent != "director" {
		t.Fatalf("director agent = %q, want director", cfg.Director.Agent)
	}
	if got := cfg.Director.AgentConfig.Model; got != "openrouter:minimax/minimax-m2" {
		t.Fatalf("director model = %q, want the configured engine", got)
	}
}

func TestBuildProjectConfigDirectorFlags(t *testing.T) {
	cfg, err := buildProjectConfig(projectSetConfigOptions{
		directorAgent: "director",
		directorModel: "openai:gpt-5",
	})
	if err != nil {
		t.Fatalf("buildProjectConfig: %v", err)
	}
	if cfg.Director.Agent != "director" {
		t.Fatalf("director agent = %q, want director", cfg.Director.Agent)
	}
	if cfg.Director.AgentConfig.Model != "openai:gpt-5" {
		t.Fatalf("director model = %q, want openai:gpt-5", cfg.Director.AgentConfig.Model)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/cli/ -run TestBuildProjectConfig`
Expected: FAIL — `agentConfig` has no `Command` field, `projectConfig` has no `Director`, `projectSetConfigOptions` has no `directorAgent`/`directorModel`.

- [ ] **Step 3: Fix the DTO gap**

In `backend/internal/cli/project.go`, add the missing field to `agentConfig` (line ~77):

```go
type agentConfig struct {
	Model       string   `json:"model,omitempty"`
	Permissions string   `json:"permissions,omitempty"`
	// Command mirrors domain.AgentConfig.Command. Its absence silently dropped
	// any command supplied via --config-json, because encoding/json discards
	// fields the target struct does not declare.
	Command []string `json:"command,omitempty"`
}
```

Add `Director` to `projectConfig` (line ~99), next to `Orchestrator`:

```go
	Director roleOverride `json:"director,omitempty"`
```

- [ ] **Step 4: Add the flags**

Add to `projectSetConfigOptions`:

```go
	directorAgent string
	directorModel string
```

In `buildProjectConfig`'s flag-built config literal, add:

```go
		Director: roleOverride{
			Agent:       opts.directorAgent,
			AgentConfig: agentConfig{Model: opts.directorModel},
		},
```

Register the flags where the sibling `--worker-agent`/`--orchestrator-agent` flags are declared (find them in the `set-config` command setup around line 271):

```go
	cmd.Flags().StringVar(&opts.directorAgent, "director-agent", "", "Harness that drives work cards as the project's Director")
	cmd.Flags().StringVar(&opts.directorModel, "director-model", "", `Director engine as "provider:model-name" (e.g. openai:gpt-5)`)
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `cd backend && go test ./internal/cli/ -v`
Expected: PASS — both new tests plus every existing CLI test.

- [ ] **Step 6: Regenerate the API artifacts**

The `Director` field is now part of the config DTO the daemon serves. Run from the repo root: `npm run api`
Expected: `backend/internal/httpd/apispec/openapi.yaml` and `frontend/src/api/schema.ts` gain the `director` property on the project-config schema. If neither file changes, the config type is not reflected in the spec — note that in your report and continue.

- [ ] **Step 7: Verify the full backend**

Run: `cd backend && go build ./... && go test ./...`
Expected: PASS except the three known-red tests.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/cli/project.go backend/internal/cli/project_test.go backend/internal/httpd/apispec/openapi.yaml frontend/src/api/schema.ts
git commit -m "fix(cli): stop dropping config fields and add Director flags"
```

---

### Task 12: Frontend — recognize a non-Hermes commander

**Files:**
- Modify: `frontend/src/renderer/components/WorkCardFocusPanel.tsx`
- Test: `frontend/src/renderer/components/WorkCardFocusPanel.test.tsx`

**Interfaces:**
- Consumes: nothing from earlier tasks (pure renderer change).
- Produces: commander UI that keys off `session.kind === "orchestrator"` alone.

- [ ] **Step 1: Write the failing test**

Add to `frontend/src/renderer/components/WorkCardFocusPanel.test.tsx`, matching the file's existing render helpers and mocks:

```tsx
it("renders commander UI for a Director session, not just Hermes", () => {
	renderPanel({
		card: { ...baseCard, status: "running", sessionId: "director-1" },
		session: { id: "director-1", kind: "orchestrator", harness: "director" },
	});
	expect(screen.getByText("Work owner")).toBeInTheDocument();
	expect(screen.queryByText("Linked session")).not.toBeInTheDocument();
});
```

Read the existing tests in that file first and reuse their exact `renderPanel`/fixture names — if the helper has a different name or signature, adapt the call rather than introducing a new helper.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd frontend && npx vitest run src/renderer/components/WorkCardFocusPanel.test.tsx`
Expected: FAIL — "Work owner" is not rendered, because the panel requires `harness === "hermes"`.

- [ ] **Step 3: Generalize the predicate**

In `frontend/src/renderer/components/WorkCardFocusPanel.tsx`, replace line 68:

```tsx
	const isHermesCommander = session?.kind === "orchestrator" && session.harness === "hermes";
```

with:

```tsx
	// Any orchestrator-kind session is this card's commander — Hermes, the
	// Director agent, or anything else configured for the role.
	const isCommander = session?.kind === "orchestrator";
```

Update the comment above `canShowTerminal` (lines 61-63) to say "A commander" instead of "A Hermes commander".

Then replace every remaining `isHermesCommander` reference with `isCommander`, neutralizing the copy:

- Line 196: `{isCommander ? "Nudge commander" : "Nudge agent"}` (unchanged apart from the rename)
- Line 224: `{isCommander ? "Work owner" : "Linked session"}` (rename only)
- Line 226: replace `Hermes coordinates this task` with `{session.harness} coordinates this task` so the actual agent is named
- Line 228: replace `Hermes stays responsible through review and testing.` with `The commander stays responsible through review and testing.`
- Line 295: `{showTerminal ? "Hide live terminal" : isCommander ? "Show commander terminal" : "Show live terminal"}`
- Line 317: `{isCommander ? "Commander terminal" : "Live terminal"}`
- Line 318: unchanged text, rename only

Leave lines 240-242's dispatch-failure strings **as they are** — they read the wire enum values, which Global Constraints forbid changing, and neutralizing only their display text is not required for this feature to work.

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd frontend && npx vitest run src/renderer/components/WorkCardFocusPanel.test.tsx`
Expected: PASS — the new test plus every pre-existing test in the file (the Hermes-session tests must still pass, proving the generalization did not regress that path).

- [ ] **Step 5: Verify the frontend typechecks**

Run from the repo root: `npm run frontend:typecheck`
Expected: PASS — no `isHermesCommander` references remain.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/renderer/components/WorkCardFocusPanel.tsx frontend/src/renderer/components/WorkCardFocusPanel.test.tsx
git commit -m "feat(frontend): show commander UI for any orchestrator session"
```

---

### Task 13: End-to-end verification against a live daemon

Automated tests do not cover daemon → dispatch → Director process → `ao` CLI → daemon. This task is the only evidence the feature works.

**Files:**
- Modify: `memory-bank/activeContext.md`, `memory-bank/progress.md`, `docs/STATUS.md`

- [ ] **Step 1: Run the full local verification suite**

Run from the repo root:

```bash
npm run lint
npm run frontend:typecheck
cd director && npm test && npm run build
```

Expected: all pass, except the three known-red `TestLifecycleDispatcherIsUnwired_*` tests in `npm run lint`'s Go suite.

- [ ] **Step 2: Start an isolated daemon**

Never touch `~/.ao` — a real user daemon may be running. Build and start against a scratch data dir, following the pattern used by this repo's previous live smoke tests:

```bash
cd backend && go build -o /tmp/ao_director ./cmd/ao
SCRATCH=$(mktemp -d)
AO_DATA_DIR=$SCRATCH/data AO_RUN_FILE=$SCRATCH/run/running.json AO_PORT=38160 /tmp/ao_director daemon &
```

Confirm it is ready with `AO_DATA_DIR=$SCRATCH/data AO_RUN_FILE=$SCRATCH/run/running.json AO_PORT=38160 /tmp/ao_director status`, and confirm `$SCRATCH/data/director/index.js` exists (Task 7's install ran).

- [ ] **Step 3: Register a project with the Director configured**

Create a throwaway git repo, register it, and set the Director config using the new flags:

```bash
/tmp/ao_director project add --path $SCRATCH/repo --id dtest --name "director test"
/tmp/ao_director project set-config dtest --director-agent director --director-model "anthropic:claude-sonnet-4-6"
```

Confirm it persisted (this is the bug Task 11 fixed): read the config back with `/tmp/ao_director project get dtest --json` (or the closest available command) and verify the `director` block is present. If it is absent, stop — Task 11's fix is incomplete.

Supply the provider API key through the project env so the Director can authenticate, e.g. `--env ANTHROPIC_API_KEY=...` on `set-config` (check the flag's real name with `--help`). Do not paste a key into any file or commit.

- [ ] **Step 4: Create a card and confirm the Director spawns**

Create a Todo card on the project, then confirm within ~30 seconds that a session appears with `kind=orchestrator` and `harness=director` (`ao session ls --project dtest --json`).

- [ ] **Step 5: Confirm the Director drives the card**

Watch the card's status and the daemon log. Expected: the Director reads the card, and either advances it (`running → review → …`) or blocks it with a recorded reason. **Either outcome is a pass** — a blocked card with a clear reason is the designed behavior for ambiguous work, and is the specific improvement over the stuck-Hermes failure this feature exists to fix. A card that sits in one status with no transition and no blocked reason is a **failure**.

- [ ] **Step 6: Confirm the UI shows the commander**

With the daemon running, open the Director page and confirm the card's focus panel shows "Work owner" and "director coordinates this task" rather than "Linked session".

If the desktop app cannot be launched in this environment, say so plainly in your report and rely on Task 12's component test instead — do not claim UI verification that did not happen.

- [ ] **Step 7: Clean up**

Stop the daemon, kill any tmux sessions and child processes it spawned, and confirm nothing under `~/.ao` was created or modified. Do not commit the scratch repo or data dir.

- [ ] **Step 8: Record the result**

Update `memory-bank/activeContext.md`, `memory-bank/progress.md`, and `docs/STATUS.md` with what was verified and what was not. Be explicit about the one known limitation carried over from the spec: the Director has no auto-answer, stall-nudge, or rate-limit-switch parity with the Hermes commander.

If any step failed, record the exact failure under Blockers and stop — do not mark the feature verified.

- [ ] **Step 9: Commit**

```bash
git add memory-bank/activeContext.md memory-bank/progress.md docs/STATUS.md
git commit -m "docs: record the Director agent end-to-end verification"
```

---

## Out of scope (from the spec, not built here)

- Auto-answer routing for an idle Director (`answer.go` is Hermes-only).
- Stall-nudging an idle Director (`stall_nudge.go` is Hermes-only).
- Excluding the Director from rate-limit auto-switch/kill (`switch_agent.go`).
- Renaming backend Hermes identifiers or changing the `hermes_unavailable`/`non_hermes_orchestrator` enum values.
- Any change to `commander/orchestrator/*`.
- Adding `--model` forwarding to the `codex`/`opencode`/`kimi`/`qwen` adapters.
- A desktop-UI control for the Director config.
- `CreateWorkCardDialog.tsx:290`'s hardcoded coding-agent chip.
- Updating `internal/skillassets/using-ao/`'s Hermes-specific wording.
- DeepAgents' filesystem/sandbox backends and MCP tool support.
- Automating the `director/dist` → `directorassets/bundle` copy in the build.
- A per-card `dispatch_failed` event with a `director_unavailable` reason (see "One deliberate divergence" above) — the Director spawns a project-level commander with no card claimed, so there is no card to attach the event to.
