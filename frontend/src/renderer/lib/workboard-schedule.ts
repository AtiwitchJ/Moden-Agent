export function formatDatetimeLocalValue(date: Date): string {
	const pad = (value: number) => String(value).padStart(2, "0");
	return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

export function parseDatetimeLocalValue(value: string): string | undefined {
	const trimmed = value.trim();
	if (!trimmed) return undefined;
	const parsed = new Date(trimmed);
	if (Number.isNaN(parsed.getTime())) return undefined;
	return parsed.toISOString();
}

export function defaultScheduleValue(from = new Date()): string {
	const next = new Date(from);
	next.setMinutes(next.getMinutes() + 60 - (next.getMinutes() % 60), 0, 0);
	return formatDatetimeLocalValue(next);
}

export function formatScheduledAtDisplay(iso: string): string {
	return new Date(iso).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}
