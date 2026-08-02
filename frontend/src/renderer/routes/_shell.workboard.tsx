import { createFileRoute, redirect } from "@tanstack/react-router";

// The global Workboard moved to Director mode at /manage. Keep this
// route only as a redirect so pre-mode deep links and any stored navigation
// state keep resolving.
export const Route = createFileRoute("/_shell/workboard")({
	beforeLoad: () => {
		throw redirect({ to: "/manage" });
	},
});
