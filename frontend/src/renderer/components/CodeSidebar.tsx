import { useNavigate, useRouterState } from "@tanstack/react-router";
import { GitPullRequest, Moon, Plus, Settings, Sun, Terminal } from "lucide-react";
import { recentSessions } from "../lib/recent-sessions";
import { formatRelativeTime } from "../lib/relative-time";
import { cn } from "../lib/utils";
import { useUiStore } from "../stores/ui-store";
import type { WorkspaceSummary } from "../types/workspace";
import aoLogo from "../assets/ao-logo.png";
import { SessionDot } from "./SessionDot";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "./ui/dropdown-menu";
import {
	Sidebar as SidebarRoot,
	SidebarContent,
	SidebarFooter,
	SidebarGroup,
	SidebarGroupContent,
	SidebarGroupLabel,
	SidebarHeader,
	SidebarMenu,
	SidebarMenuButton,
	SidebarMenuItem,
	SidebarTrigger,
} from "./ui/sidebar";

const RECENTS_LIMIT = 20;
const isMac = typeof navigator !== "undefined" && /Mac|iPod|iPhone|iPad/.test(navigator.userAgent);

/** DOM id CodeHomeComposer tags its prompt input with, so New (below) can
 * focus it without any shared state — just navigate home, then focus. */
export const CODE_HOME_COMPOSER_INPUT_ID = "code-home-composer-input";

export type CodeSidebarProps = {
	workspaces: WorkspaceSummary[];
};

// Replaces the project/company tree in Code mode: a flat New / Recents / More
// nav modeled on the Claude Code desktop app. Project/company selection moved
// into CodeHomeComposer's picker and the New flow — see
// docs/superpowers/specs/2026-08-02-moden-code-home-design.md.
export function CodeSidebar({ workspaces }: CodeSidebarProps) {
	const navigate = useNavigate();
	const pathname = useRouterState({ select: (state) => state.location.pathname });
	const theme = useUiStore((s) => s.theme);
	const toggleTheme = useUiStore((s) => s.toggleTheme);
	const recents = recentSessions(workspaces, RECENTS_LIMIT);

	const goNew = () => {
		if (pathname !== "/") void navigate({ to: "/" });
		requestAnimationFrame(() => document.getElementById(CODE_HOME_COMPOSER_INPUT_ID)?.focus());
	};

	return (
		<SidebarRoot collapsible="icon" className="border-border top-14 h-[calc(100svh-3.5rem)]!">
			<SidebarHeader className="gap-0 p-0 pl-2.5 pr-[7px] pt-3.5 group-data-[collapsible=icon]:px-1.5">
				<div className="flex shrink-0 items-center gap-1 px-2 pb-[18px] group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0 group-data-[collapsible=icon]:pb-2">
					<button
						aria-label="Home"
						className="flex h-9 min-w-0 flex-1 items-center gap-2.5 rounded-[5px] px-1.5 transition-colors hover:bg-interactive-hover group-data-[collapsible=icon]:size-9 group-data-[collapsible=icon]:flex-none group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:rounded-lg group-data-[collapsible=icon]:px-0"
						onClick={() => void navigate({ to: "/" })}
						type="button"
					>
						<img alt="" aria-hidden="true" className="h-[22px] w-[22px] shrink-0 rounded-[6px] object-cover" src={aoLogo} />
						<span className="min-w-0 flex-1 truncate text-left text-[14px] font-bold tracking-[-0.015em] text-foreground group-data-[collapsible=icon]:hidden">
							Modern Agent
						</span>
					</button>
					{!isMac && (
						<SidebarTrigger className="size-[18px] shrink-0 rounded-[4px] p-0 text-passive hover:bg-interactive-hover hover:text-foreground group-data-[collapsible=icon]:hidden [&_svg]:size-[15px]" />
					)}
				</div>
			</SidebarHeader>

			<SidebarContent className="gap-0 pl-2.5 pr-[7px] group-data-[collapsible=icon]:items-center group-data-[collapsible=icon]:px-1.5">
				<SidebarGroup className="p-0">
					<SidebarMenu className="gap-0.5">
						<SidebarMenuItem>
							<SidebarMenuButton
								className="h-9 gap-2.5 rounded-[5px] px-2 text-[13px] font-medium text-foreground hover:bg-interactive-hover"
								onClick={goNew}
								tooltip="New"
							>
								<Plus aria-hidden="true" className="size-4" />
								<span className="group-data-[collapsible=icon]:hidden">New</span>
							</SidebarMenuButton>
						</SidebarMenuItem>
					</SidebarMenu>
				</SidebarGroup>

				<SidebarGroup className="mt-2 p-0 group-data-[collapsible=icon]:hidden">
					<SidebarGroupLabel className="h-auto rounded-none px-2 pb-2 text-[10.5px] font-semibold uppercase tracking-[0.09em] text-passive">
						Recents
					</SidebarGroupLabel>
					<SidebarGroupContent>
						<SidebarMenu className="gap-0">
							{recents.length === 0 ? (
								<p className="px-2 py-1.5 text-[12px] text-passive">No sessions yet.</p>
							) : (
								recents.map((session) => (
									<SidebarMenuItem key={session.id}>
										<SidebarMenuButton
											className="h-auto items-start gap-2 rounded-[4px] px-2 py-1.5"
											onClick={() =>
												void navigate({
													to: "/projects/$projectId/sessions/$sessionId",
													params: { projectId: session.workspaceId, sessionId: session.id },
												})
											}
										>
											<SessionDot session={session} />
											<span className="min-w-0 flex-1">
												<span className="block truncate text-[12px] text-foreground">{session.title}</span>
												<span className="block text-[11px] text-passive">{formatRelativeTime(session.updatedAt)}</span>
											</span>
										</SidebarMenuButton>
									</SidebarMenuItem>
								))
							)}
						</SidebarMenu>
					</SidebarGroupContent>
				</SidebarGroup>
			</SidebarContent>

			<SidebarFooter className="mt-auto gap-0 border-t border-border p-[7px] group-data-[collapsible=icon]:items-center group-data-[collapsible=icon]:px-1.5">
				<DropdownMenu>
					<DropdownMenuTrigger asChild>
						<button
							aria-label="More"
							className={cn(
								"flex w-full items-center justify-start gap-2.5 rounded-md p-2 text-[13px] font-medium text-passive transition-colors hover:bg-interactive-hover hover:text-foreground [&_svg]:size-[15px] [&_svg]:text-passive",
								"group-data-[collapsible=icon]:size-9 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:p-0",
							)}
							type="button"
						>
							<Settings aria-hidden="true" />
							<span className="group-data-[collapsible=icon]:hidden">More</span>
						</button>
					</DropdownMenuTrigger>
					<DropdownMenuContent align="start" className="w-56 min-w-0" side="top">
						<DropdownMenuItem onSelect={toggleTheme}>
							{theme === "dark" ? <Sun aria-hidden="true" /> : <Moon aria-hidden="true" />}
							{theme === "dark" ? "Light mode" : "Dark mode"}
						</DropdownMenuItem>
						<DropdownMenuSeparator />
						<DropdownMenuItem onSelect={() => void navigate({ to: "/prs" })}>
							<GitPullRequest aria-hidden="true" />
							Pull requests
						</DropdownMenuItem>
						<DropdownMenuItem onSelect={() => void navigate({ to: "/terminals", search: { sessions: "" } })}>
							<Terminal aria-hidden="true" />
							Live Terminals
						</DropdownMenuItem>
						<DropdownMenuSeparator />
						<DropdownMenuItem onSelect={() => void navigate({ to: "/settings" })}>
							<Settings aria-hidden="true" />
							Settings
						</DropdownMenuItem>
					</DropdownMenuContent>
				</DropdownMenu>
			</SidebarFooter>
		</SidebarRoot>
	);
}
