import { useNavigate, useRouterState } from "@tanstack/react-router";
import { cn } from "../lib/utils";

const isMac = typeof navigator !== "undefined" && /Mac|iPod|iPhone|iPad/.test(navigator.userAgent);
const dragStyle = isMac ? ({ WebkitAppRegion: "drag" } as React.CSSProperties) : undefined;
const noDragStyle = isMac ? ({ WebkitAppRegion: "no-drag" } as React.CSSProperties) : undefined;

export type AppMode = "code" | "manage" | "work";

export function activeModeFromPathname(pathname: string): AppMode {
	if (pathname === "/manage" || pathname.startsWith("/manage/")) return "manage";
	if (pathname === "/work" || pathname.startsWith("/work/")) return "work";
	return "code";
}

const MODES: { id: AppMode; label: string; to: string }[] = [
	{ id: "code", label: "Code", to: "/" },
	{ id: "manage", label: "Code Manage", to: "/manage" },
	{ id: "work", label: "Work", to: "/work" },
];

// Topmost window strip: hosts the macOS traffic-light inset (left 79px, same
// as the TitlebarNav cluster budget) and the three mode tabs. The whole strip
// is a drag region; tabs punch no-drag holes. TitlebarNav (fixed, top-left)
// overlays this strip — see .titlebar-nav in styles.css (height 40px).
export function ModeBar() {
	const navigate = useNavigate();
	const pathname = useRouterState({ select: (state) => state.location.pathname });
	const active = activeModeFromPathname(pathname);

	return (
		<div
			className={cn(
				"flex h-10 shrink-0 items-center justify-center border-b border-border bg-background",
				isMac && "pl-[167px] pr-4", // traffic lights + TitlebarNav cluster live in this inset
			)}
			style={dragStyle}
		>
			<div className="flex items-center gap-1 rounded-lg bg-interactive-hover/50 p-0.5" style={noDragStyle}>
				{MODES.map((mode) => (
					<button
						key={mode.id}
						aria-current={active === mode.id ? "page" : undefined}
						className={cn(
							"rounded-md px-3 py-1 text-[12px] font-medium transition-colors",
							active === mode.id
								? "bg-interactive-active text-foreground"
								: "text-passive hover:bg-interactive-hover hover:text-foreground",
						)}
						onClick={() => void navigate({ to: mode.to })}
						type="button"
					>
						{mode.label}
					</button>
				))}
			</div>
		</div>
	);
}
