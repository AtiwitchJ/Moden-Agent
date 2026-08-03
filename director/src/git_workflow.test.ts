import { describe, expect, it, vi } from "vitest";
import { loadGitWorkflowSkill } from "./git_workflow.js";

describe("loadGitWorkflowSkill", () => {
	it("loads the packaged Git workflow instructions from the Director skills directory", async () => {
		const read = vi.fn().mockResolvedValue("# Git Workflow\nUse atomic commits.");
		await expect(loadGitWorkflowSkill({ AO_DIRECTOR_SKILLS_DIR: "/skills" }, read)).resolves.toContain("# Git Workflow");
		expect(read).toHaveBeenCalledWith("/skills/git-workflow-and-versioning/SKILL.md", "utf8");
	});

	it("does not make a Director launch fail when an optional skill is unavailable", async () => {
		await expect(loadGitWorkflowSkill({}, vi.fn())).resolves.toBe("");
	});
});
