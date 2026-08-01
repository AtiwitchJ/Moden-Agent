import { describe, expect, it } from "vitest";
import { activeModeFromPathname } from "./ModeBar";

describe("activeModeFromPathname", () => {
	it("maps /manage and nested paths to manage", () => {
		expect(activeModeFromPathname("/manage")).toBe("manage");
		expect(activeModeFromPathname("/manage/anything")).toBe("manage");
	});
	it("maps /work to work", () => {
		expect(activeModeFromPathname("/work")).toBe("work");
	});
	it("maps everything else to code", () => {
		expect(activeModeFromPathname("/")).toBe("code");
		expect(activeModeFromPathname("/projects/p1/sessions/s1")).toBe("code");
		expect(activeModeFromPathname("/workboard")).toBe("code"); // redirects to /manage before render
	});
});
