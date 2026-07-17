import { describe, expect, it } from "vitest";
import { defaultScheduleValue, formatDatetimeLocalValue, formatScheduledAtDisplay, parseDatetimeLocalValue } from "./workboard-schedule";

describe("workboard-schedule", () => {
	it("round-trips datetime-local values through ISO timestamps", () => {
		const iso = parseDatetimeLocalValue("2026-07-17T15:30");
		expect(iso).toBeTruthy();
		expect(formatDatetimeLocalValue(new Date(iso as string))).toBe("2026-07-17T15:30");
	});

	it("defaults the next schedule slot to the upcoming hour", () => {
		const from = new Date("2026-07-17T09:17:00");
		expect(defaultScheduleValue(from)).toBe("2026-07-17T10:00");
	});

	it("formats scheduled timestamps for card display", () => {
		expect(formatScheduledAtDisplay("2026-07-17T09:00:00.000Z")).toMatch(/2026/);
	});
});
