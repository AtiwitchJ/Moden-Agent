import { readFile } from "node:fs/promises";
import { join } from "node:path";

type ReadSkill = (path: string, encoding: "utf8") => Promise<string>;

// loadGitWorkflowSkill reads the packaged skill at runtime instead of
// duplicating its instructions in code. Director's own prompt keeps the
// destructive-action guardrails authoritative over this optional skill.
export async function loadGitWorkflowSkill(
	env: NodeJS.ProcessEnv,
	read: ReadSkill = readFile,
): Promise<string> {
	const root = env.AO_DIRECTOR_SKILLS_DIR?.trim();
	if (!root) return "";
	try {
		const skill = await read(join(root, "git-workflow-and-versioning", "SKILL.md"), "utf8");
		return `## Installed skill: Git Workflow and Versioning\n\n${skill}`;
	} catch (error) {
		console.warn(`director: Git workflow skill unavailable: ${error instanceof Error ? error.message : String(error)}`);
		return "";
	}
}
