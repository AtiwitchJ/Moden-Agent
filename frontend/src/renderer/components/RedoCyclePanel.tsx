import { ChevronDown, ChevronRight, RotateCcw } from "lucide-react";
import { useState } from "react";
import type { components } from "../../api/schema";
import { RedoFindingList } from "./RedoFindingList";
import { cn } from "../lib/utils";

export type RedoCycle = components["schemas"]["RedoCycleResponse"] & {
	status?: string;
};

export function RedoCyclePanel({ cycles, latestSummary }: { cycles: RedoCycle[]; latestSummary?: string }) {
	const [openCycleIds, setOpenCycleIds] = useState<Record<string, boolean>>(() => {
		if (cycles.length > 0) {
			const latest = cycles[cycles.length - 1];
			return { [latest.id]: true };
		}
		return {};
	});

	if (cycles.length === 0 && !latestSummary) {
		return null;
	}

	const toggleCycle = (id: string) => {
		setOpenCycleIds((prev) => ({ ...prev, [id]: !prev[id] }));
	};

	return (
		<div className="space-y-3 rounded-md border border-red-500/30 bg-red-500/5 p-3 text-foreground">
			<div className="flex items-center justify-between">
				<div className="flex items-center gap-2">
					<RotateCcw className="size-4 text-red-500" />
					<h3 className="text-[13px] font-semibold text-foreground">Redo History ({cycles.length} cycle{cycles.length === 1 ? "" : "s"})</h3>
				</div>
			</div>

			{latestSummary ? (
				<p className="text-[12px] text-muted-foreground border-l-2 border-red-500/40 pl-2 py-0.5">
					{latestSummary}
				</p>
			) : null}

			<div className="space-y-2">
				{cycles.slice().reverse().map((cycle) => {
					const isOpen = openCycleIds[cycle.id] ?? false;
					const findingsList = cycle.findings ?? [];
					const statusLabel = cycle.status || (cycle.completedAt ? "passed" : "in_progress");
					return (
						<div key={cycle.id} className="rounded border border-border bg-background overflow-hidden">
							<button
								type="button"
								onClick={() => toggleCycle(cycle.id)}
								className="w-full flex items-center justify-between px-3 py-2 text-left hover:bg-muted/30 transition-colors"
							>
								<div className="flex items-center gap-2 text-[12px]">
									{isOpen ? <ChevronDown className="size-3.5 text-muted-foreground" /> : <ChevronRight className="size-3.5 text-muted-foreground" />}
									<span className="font-semibold text-foreground">Cycle #{cycle.cycleNumber}</span>
									<span className={cn(
										"rounded px-1.5 py-0.2 text-[10px] uppercase font-mono font-medium",
										statusLabel === "failed" && "bg-red-500/10 text-red-500",
										statusLabel === "in_progress" && "bg-amber-500/10 text-amber-500",
										statusLabel === "passed" && "bg-green-500/10 text-green-500"
									)}>
										{statusLabel}
									</span>
									<span className="text-[10px] text-muted-foreground font-mono">
										via {cycle.source}
									</span>
								</div>
								<span className="text-[11px] text-muted-foreground">
									{findingsList.length} finding{findingsList.length === 1 ? "" : "s"}
								</span>
							</button>

							{isOpen ? (
								<div className="border-t border-border p-3 space-y-2 bg-card/40">
									{cycle.summary ? (
										<div className="text-[11px] text-muted-foreground">
											<span className="font-medium text-foreground">Summary: </span>
											{cycle.summary}
										</div>
									) : null}
									<RedoFindingList findings={findingsList} />
								</div>
							) : null}
						</div>
					);
				})}
			</div>
		</div>
	);
}
