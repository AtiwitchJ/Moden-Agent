import { createFileRoute } from "@tanstack/react-router";
import { Workboard } from "../components/Workboard";

export const Route = createFileRoute("/_shell/workboard")({
	component: GlobalWorkboardPage,
});

function GlobalWorkboardPage() {
	return <Workboard />;
}
