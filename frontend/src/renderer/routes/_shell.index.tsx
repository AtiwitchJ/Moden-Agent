import { createFileRoute } from "@tanstack/react-router";
import { CEODashboard } from "../components/CEODashboard";

// Temporary: still renders the relocated CEODashboard. Task 8 replaces this
// with CodeHome (see docs/superpowers/plans/2026-08-02-moden-code-home.md).
export const Route = createFileRoute("/_shell/")({
	component: CEODashboard,
});
