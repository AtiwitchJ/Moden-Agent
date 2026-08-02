import { createRootRouteWithContext, Outlet, useRouterState } from "@tanstack/react-router";
import { useEffect, useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { TooltipProvider } from "../components/ui/tooltip";
import { ModeBar } from "../components/ModeBar";
import { TitlebarNav } from "../components/TitlebarNav";
import type { QueryClient } from "@tanstack/react-query";
import { captureRendererEvent, routeSurface } from "../lib/telemetry";
import { useDaemonStatus } from "../hooks/useDaemonStatus";
import { ShellProvider } from "../lib/shell-context";

export const Route = createRootRouteWithContext<{
	queryClient: QueryClient;
}>()({
	component: RootComponent,
});

export function RootComponent() {
	const location = useRouterState({ select: (state) => state.location });
	const queryClient = useQueryClient();
	const daemonStatus = useDaemonStatus(queryClient);

	useEffect(() => {
		void captureRendererEvent("ao.renderer.route_viewed", {
			surface: routeSurface(location.pathname),
		});
	}, [location.pathname]);

	const shellContextValue = useMemo(
		() => ({
			daemonStatus,
			createProject: async () => {
				throw new Error("createProject is not available in this context");
			},
			removeProject: async () => {
				throw new Error("removeProject is not available in this context");
			},
		}),
		[daemonStatus],
	);

	return (
		<TooltipProvider>
			<ShellProvider value={shellContextValue}>
				<div className="flex h-screen min-h-0 flex-col bg-background text-foreground">
					<ModeBar />
					<div className="min-h-0 flex-1">
						<Outlet />
					</div>
				</div>
				{/* Fixed macOS titlebar cluster beside the traffic lights — rendered here
				    (outside the outlet, after the main layout content) so it's always
				    available in all modes (Code, Director, Work) without re-mounting.
				    MUST come after the ModeBar and content div in the DOM: Electron
				    builds the window-drag region in document order (drag rects add,
				    no-drag rects subtract), so the cluster's no-drag holes only survive
				    if they're processed after the drag strips they overlap. If reordered
				    before, clicks on cluster buttons get swallowed by window-drag even
				    though DOM hit-testing looks correct. */}
				<TitlebarNav />
			</ShellProvider>
		</TooltipProvider>
	);
}
