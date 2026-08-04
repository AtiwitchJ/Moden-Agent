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

	it("builds the spawn-worker argv as a blocking one-shot run", () => {
		expect(buildSpawnWorkerArgv("proj-1", "claude-code", "implement X")).toEqual([
			"spawn", "--project", "proj-1", "--agent", "claude-code", "--name", "claude-code-worker",
			"--oneshot", "--wait", "--prompt", "implement X",
		]);
	});

	it("truncates the spawn-worker display name to 20 characters", () => {
		const argv = buildSpawnWorkerArgv("proj-1", "some-very-long-harness-id", "implement X");
		const name = argv[argv.indexOf("--name") + 1];
		expect(name.length).toBeLessThanOrEqual(20);
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
