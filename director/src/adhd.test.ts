import { describe, expect, it, vi } from "vitest";
import { loadADHDSkill } from "./adhd.js";

describe("loadADHDSkill", () => {
	it("loads the packaged ADHD instructions from the Director skills directory", async () => {
		const read = vi.fn().mockResolvedValue("# ADHD\nThink divergently.");
		await expect(loadADHDSkill({ AO_DIRECTOR_SKILLS_DIR: "/skills" }, read)).resolves.toContain("# ADHD");
		expect(read).toHaveBeenCalledWith("/skills/adhd/SKILL.md", "utf8");
	});

	it("does not make a Director launch fail when an optional skill is unavailable", async () => {
		await expect(loadADHDSkill({}, vi.fn())).resolves.toBe("");
	});
});
