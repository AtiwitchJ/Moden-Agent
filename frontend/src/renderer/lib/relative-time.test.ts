import { describe, expect, it } from "vitest";
import { formatRelativeTime } from "./relative-time";

describe("formatRelativeTime", () => {
	const now = new Date("2026-08-02T12:00:00Z");

	it("returns 'just now' for under 5 seconds", () => {
		expect(formatRelativeTime("2026-08-02T11:59:58Z", now)).toBe("just now");
	});

	it("formats minutes", () => {
		expect(formatRelativeTime("2026-08-02T11:55:00Z", now)).toBe("5 minutes ago");
	});

	it("formats hours", () => {
		expect(formatRelativeTime("2026-08-02T09:00:00Z", now)).toBe("3 hours ago");
	});

	it("formats days", () => {
		expect(formatRelativeTime("2026-07-31T12:00:00Z", now)).toBe("2 days ago");
	});

	it("falls back to a locale date past a year", () => {
		expect(formatRelativeTime("2020-01-01T00:00:00Z", now)).toBe(new Date("2020-01-01T00:00:00Z").toLocaleDateString());
	});
});
