import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/work")({
	component: WorkPlaceholderPage,
});

// Moden Work mode has no spec yet; this page only reserves the route so the
// ModeBar tab has somewhere to land. Do not build features here.
function WorkPlaceholderPage() {
	return (
		<div className="flex h-full items-center justify-center">
			<div className="text-center">
				<h1 className="text-[15px] font-semibold text-foreground">Work</h1>
				<p className="mt-1 text-[13px] text-passive">Coming soon.</p>
			</div>
		</div>
	);
}
