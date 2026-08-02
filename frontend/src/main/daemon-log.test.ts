// @vitest-environment node
import { mkdtemp, readFile, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { createDaemonLog } from "./daemon-log";

describe("createDaemonLog", () => {
	let tmpDir: string;

	beforeEach(async () => {
		tmpDir = await mkdtemp(path.join(os.tmpdir(), "ao-daemon-log-"));
	});

	afterEach(async () => {
		// Best-effort cleanup; ignore errors on Windows locked handles.
		await import("node:fs/promises").then(({ rm }) =>
			rm(tmpDir, { recursive: true, force: true }).catch(() => undefined),
		);
	});

	it("creates the data directory and active log file", async () => {
		const log = await createDaemonLog({ dataDir: tmpDir });
		log.write("first line\n");
		await log.close();

		const written = await readFile(path.join(tmpDir, "daemon.log"), "utf8");
		expect(written).toContain("first line");
	});

	it("buffers partial lines until a newline arrives", async () => {
		const log = await createDaemonLog({ dataDir: tmpDir });
		log.write("part one ");
		log.write("part two\n");
		await log.close();

		const written = await readFile(path.join(tmpDir, "daemon.log"), "utf8");
		expect(written).toContain("part one part two");
	});

	it("returns recent lines from memory via readTail", async () => {
		const log = await createDaemonLog({ dataDir: tmpDir, memoryLines: 3 });
		log.write("a\nb\nc\nd\n");
		await log.close();

		const tail = await log.readTail(2);
		expect(tail).toEqual(["c", "d"]);
	});

	it("rotates the active file when it exceeds maxBytes", async () => {
		const log = await createDaemonLog({ dataDir: tmpDir, maxBytes: 20, maxFiles: 1, memoryLines: 10 });
		log.write("line one\n");
		log.write("line two is longer\n");
		log.write("line three\n");
		await log.close();

		const active = await readFile(path.join(tmpDir, "daemon.log"), "utf8");
		const archive = await readFile(path.join(tmpDir, "daemon.log.1"), "utf8");
		expect(active).toContain("line three");
		expect(archive).toContain("line one");
		expect(archive).not.toContain("line three");
	});

	it("falls back to reading the disk file when memory has fewer lines than requested", async () => {
		const filePath = path.join(tmpDir, "daemon.log");
		await writeFile(filePath, "alpha\nbeta\ngamma\n", "utf8");
		const log = await createDaemonLog({ dataDir: tmpDir, memoryLines: 1 });
		log.write("delta\n");
		await log.close();

		const tail = await log.readTail(3);
		expect(tail).toEqual(["beta", "gamma", "delta"]);
	});
});
