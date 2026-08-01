import { createRootRouteWithContext, Outlet, useRouterState } from "@tanstack/react-router";
import { useEffect } from "react";
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

function RootComponent() {
	const location = useRouterState({ select: (state) => state.location });
	const queryClient = useQueryClient();
	const daemonStatus = useDaemonStatus(queryClient);

	useEffect(() => {
		void captureRendererEvent("ao.renderer.route_viewed", {
			surface: routeSurface(location.pathname),
		});
	}, [location.pathname]);

	return (
		<TooltipProvider>
			<ShellProvider
				value={{
					daemonStatus,
					createProject: async () => {
						throw new Error("createProject is not available in this context");
					},
				}}
			>
				<div className="flex h-screen min-h-0 flex-col bg-background text-foreground">
					<ModeBar />
					<div className="min-h-0 flex-1">
						<Outlet />
					</div>
				</div>
				{/* Fixed macOS titlebar cluster beside the traffic lights. Rendered here
				    (outside Outlet) as a fixed sibling so it's always available in all
				    modes (Code, Code Manage, Work) without re-mounting. */}
				<TitlebarNav />
			</ShellProvider>
		</TooltipProvider>
	);
}
