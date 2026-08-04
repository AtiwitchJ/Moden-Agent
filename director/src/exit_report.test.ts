import { describe, expect, it, vi } from "vitest";
import { reportExited } from "./exit_report.js";
import type { Runner } from "./tools.js";

describe("reportExited", () => {
	it("calls ao session mark-exited", async () => {
		const run: Runner = vi.fn().mockResolvedValue({ code: 0, stdout: "", stderr: "" });
		await reportExited(run);
		expect(run).toHaveBeenCalledWith(["session", "mark-exited"]);
	});

	it("swallows a non-zero exit rather than throwing", async () => {
		const run: Runner = vi.fn().mockResolvedValue({ code: 1, stdout: "", stderr: "connection refused" });
		await expect(reportExited(run)).resolves.toBeUndefined();
	});

	it("swallows a runner that rejects outright", async () => {
		const run: Runner = vi.fn().mockRejectedValue(new Error("spawn ao ENOENT"));
		await expect(reportExited(run)).resolves.toBeUndefined();
	});
});
