import { readFile } from "node:fs/promises";
import { join } from "node:path";

type ReadSkill = (path: string, encoding: "utf8") => Promise<string>;

// loadADHDSkill reads the packaged skill at runtime instead of duplicating it
// in code. This keeps its source and license intact, and lets Director's
// system prompt carry the complete pre-flight gate and diverge/focus workflow.
export async function loadADHDSkill(env: NodeJS.ProcessEnv, read: ReadSkill = readFile): Promise<string> {
	const root = env.AO_DIRECTOR_SKILLS_DIR?.trim();
	if (!root) return "";
	try {
		const skill = await read(join(root, "adhd", "SKILL.md"), "utf8");
		return `## Installed skill: ADHD\n\n${skill}`;
	} catch (error) {
		console.warn(`director: ADHD skill unavailable: ${error instanceof Error ? error.message : String(error)}`);
		return "";
	}
}
