const UNITS: { limit: number; divisor: number; unit: Intl.RelativeTimeFormatUnit }[] = [
	{ limit: 60, divisor: 1, unit: "second" },
	{ limit: 3600, divisor: 60, unit: "minute" },
	{ limit: 86400, divisor: 3600, unit: "hour" },
	{ limit: 604800, divisor: 86400, unit: "day" },
	{ limit: 2629800, divisor: 604800, unit: "week" },
	{ limit: 31557600, divisor: 2629800, unit: "month" },
];

const formatter = new Intl.RelativeTimeFormat("en", { numeric: "auto" });

/**
 * Compact relative time for session rows ("5 minutes ago", "2 days ago").
 * Falls back to a locale date once past a year.
 * ponytail: English only — add locale support when a second locale is needed.
 */
export function formatRelativeTime(iso: string, now: Date = new Date()): string {
	const then = Date.parse(iso);
	if (Number.isNaN(then)) return "";
	const diffSeconds = Math.round((now.getTime() - then) / 1000);
	if (diffSeconds < 5) return "just now";
	for (const { limit, divisor, unit } of UNITS) {
		if (diffSeconds < limit) return formatter.format(-Math.round(diffSeconds / divisor), unit);
	}
	return new Date(then).toLocaleDateString();
}
