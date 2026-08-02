// @vitest-environment node
//
// Bounded rotating log for the Electron-owned daemon process. The supervisor
// tees the spawned child's stderr here so a failed dispatch can be diagnosed
// without relying on the renderer having the daemon port or on terminal
// streaming. The log lives under ~/.ao/data/ alongside the daemon's SQLite store.

import { mkdir, readFile, rename, stat, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { defaultRunFilePath } from "../shared/daemon-discovery";

const DEFAULT_MAX_BYTES = 1 * 1024 * 1024; // 1 MB per file
const DEFAULT_MAX_FILES = 2; // daemon.log + daemon.log.1
const DEFAULT_MEMORY_LINES = 500; // recent lines kept in memory for fast IPC reads

export type DaemonLogOptions = {
	/** Directory that holds the log file. Defaults to ~/.ao/data. */
	dataDir?: string;
	/** Maximum size of the active log file before rotation. */
	maxBytes?: number;
	/** Number of backup files to keep (0 = no backups). */
	maxFiles?: number;
	/** Number of recent lines to keep in memory for fast tail reads. */
	memoryLines?: number;
};

/** Rotating line log for a single daemon process. */
export type DaemonLog = {
	/** Append a raw chunk of daemon output. Line buffering is handled internally. */
	write(chunk: string): void;
	/** Read the most recent lines from memory and/or disk. */
	readTail(maxLines?: number): Promise<string[]>;
	/** Flush pending writes. */
	close(): Promise<void>;
	/** Absolute path of the active log file. */
	path: string;
};

/** Resolve the canonical daemon log path under ~/.ao/data. */
export function defaultDaemonLogPath(): string {
	const runFile = defaultRunFilePath(process.platform, process.env, os.homedir());
	const aoDir = runFile ? path.dirname(runFile) : path.join(os.homedir(), ".ao");
	return path.join(aoDir, "data", "daemon.log");
}

/** Create and return a bounded rotating daemon log. */
export async function createDaemonLog(options?: DaemonLogOptions): Promise<DaemonLog> {
	const logPath = options?.dataDir ? path.join(options.dataDir, "daemon.log") : defaultDaemonLogPath();
	await mkdir(path.dirname(logPath), { recursive: true });
	return new DaemonLogImpl(logPath, options?.maxBytes ?? DEFAULT_MAX_BYTES, options?.maxFiles ?? DEFAULT_MAX_FILES, options?.memoryLines ?? DEFAULT_MEMORY_LINES);
}

class DaemonLogImpl implements DaemonLog {
	readonly path: string;
	private readonly archivePath: string;
	private readonly maxBytes: number;
	private readonly maxFiles: number;
	private readonly memoryLines: number;
	private memory: string[] = [];
	private pending = "";
	private writeQueue: Promise<void> = Promise.resolve();

	constructor(path: string, maxBytes: number, maxFiles: number, memoryLines: number) {
		this.path = path;
		this.archivePath = `${path}.1`;
		this.maxBytes = maxBytes;
		this.maxFiles = Math.max(0, maxFiles);
		this.memoryLines = memoryLines;
	}

	write(chunk: string): void {
		this.pending += chunk;
		const lines = this.pending.split("\n");
		this.pending = lines.pop() ?? "";
		for (const raw of lines) {
			const line = raw.replace(/\r$/, "");
			if (line.length === 0) continue;
			this.pushMemory(line);
			this.writeQueue = this.writeQueue.then(() => this.appendLine(line)).catch(() => undefined);
		}
	}

	async readTail(maxLines = 200): Promise<string[]> {
		if (this.memory.length >= maxLines) {
			return this.memory.slice(-maxLines);
		}
		const fileTail = await readFileTail(this.path, maxLines);
		return [...fileTail, ...this.memory].slice(-maxLines);
	}

	async close(): Promise<void> {
		await this.writeQueue;
	}

	private pushMemory(line: string): void {
		this.memory.push(line);
		if (this.memory.length > this.memoryLines) {
			this.memory.shift();
		}
	}

	private async appendLine(line: string): Promise<void> {
		const data = `${line}\n`;
		try {
			const info = await stat(this.path).catch(() => ({ size: 0 }));
			const wouldExceed = info.size + Buffer.byteLength(data, "utf8") > this.maxBytes;
			if (wouldExceed) {
				await this.rotate();
			}
			await writeFile(this.path, data, { flag: "a", encoding: "utf8" });
		} catch (err) {
			// A dropped log line is preferable to crashing the supervisor. Keep the
			// in-memory tail even if the disk write fails.
			console.error("AO: failed to write daemon log line:", err);
		}
	}

	private async rotate(): Promise<void> {
		// Simple two-file rotation: active -> .1, drop older backups. This is
		// intentionally small because the goal is recent diagnostics, not archival.
		if (this.maxFiles > 0) {
			await rename(this.path, this.archivePath).catch(() => undefined);
		}
		await writeFile(this.path, "", { encoding: "utf8" }).catch(() => undefined);
	}
}

async function readFileTail(filePath: string, maxLines: number): Promise<string[]> {
	let content: string;
	try {
		content = await readFile(filePath, "utf8");
	} catch {
		return [];
	}
	const lines = content.split("\n").filter((line) => line.length > 0);
	return lines.slice(-maxLines);
}
