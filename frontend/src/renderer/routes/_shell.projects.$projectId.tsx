import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_shell/projects/$projectId")({
	// Code mode has no project-level kanban. Session navigation goes directly to
	// a terminal, and the Code home owns starting/selecting work.
	beforeLoad: () => {
		throw redirect({ to: "/" });
	},
});
