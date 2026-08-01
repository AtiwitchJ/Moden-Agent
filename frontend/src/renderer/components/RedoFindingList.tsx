import { AlertTriangle, CheckCircle2, FileCode } from "lucide-react";
import type { components } from "../../api/schema";
import { cn } from "../lib/utils";

type RedoFinding = components["schemas"]["RedoFindingResponse"];

const severityWeights: Record<string, number> = {
	critical: 0,
	p0: 0,
	high: 1,
	p1: 1,
	normal: 2,
	p2: 2,
	low: 3,
	p3: 3,
};

const severityBadgeStyles: Record<string, string> = {
	critical: "bg-red-500/20 text-red-500 border-red-500/30",
	p0: "bg-red-500/20 text-red-500 border-red-500/30",
	high: "bg-orange-500/20 text-orange-500 border-orange-500/30",
	p1: "bg-orange-500/20 text-orange-500 border-orange-500/30",
	normal: "bg-amber-500/20 text-amber-500 border-amber-500/30",
	p2: "bg-amber-500/20 text-amber-500 border-amber-500/30",
	low: "bg-blue-500/20 text-blue-500 border-blue-500/30",
	p3: "bg-blue-500/20 text-blue-500 border-blue-500/30",
};

export function RedoFindingList({ findings }: { findings?: RedoFinding[] }) {
	const items = findings ?? [];
	const sorted = [...items].sort((a, b) => {
		const wa = severityWeights[a.severity.toLowerCase()] ?? 99;
		const wb = severityWeights[b.severity.toLowerCase()] ?? 99;
		return wa - wb;
	});

	if (sorted.length === 0) {
		return <p className="text-[12px] text-muted-foreground italic">No specific findings logged for this cycle.</p>;
	}

	return (
		<ul className="space-y-2" aria-label="Redo findings list">
			{sorted.map((finding) => {
				const isFixed = finding.status === "fixed" || finding.status === "resolved" || finding.status === "passed";
				const sevKey = finding.severity.toLowerCase();
				return (
					<li key={finding.id} className={cn("rounded-md border border-border bg-card p-2.5 text-[12px] space-y-1", isFixed && "opacity-60 bg-muted/20")}>
						<div className="flex items-center gap-2">
							<span className={cn("inline-flex items-center rounded border px-1.5 py-0.5 text-[10px] font-mono font-bold uppercase", severityBadgeStyles[sevKey] ?? severityBadgeStyles.normal)}>
								{finding.severity}
							</span>
							<span className="flex-1 font-medium text-foreground">{finding.title}</span>
							{finding.attemptCount > 0 ? (
								<span className="text-[10px] font-mono text-muted-foreground bg-muted/40 px-1 rounded">
									Attempt #{finding.attemptCount}
								</span>
							) : null}
							{isFixed ? (
								<span className="inline-flex items-center gap-1 text-[11px] text-green-500" title="Fixed">
									<CheckCircle2 className="size-3.5" />
									Fixed
								</span>
							) : (
								<span className="inline-flex items-center gap-1 text-[11px] text-amber-500" title="Open">
									<AlertTriangle className="size-3.5" />
									{finding.status}
								</span>
							)}
						</div>
						{finding.details ? <p className="text-[11px] text-muted-foreground">{finding.details}</p> : null}
						{(finding.fileRefs ?? []).map((ref, idx) => (
							<div key={idx} className="flex items-center gap-1 font-mono text-[11px] text-muted-foreground pl-0.5">
								<FileCode className="size-3" />
								<span>{ref.file}</span>
								{ref.startLine ? <span>:L{ref.startLine}{ref.endLine ? `-L${ref.endLine}` : ""}</span> : null}
							</div>
						))}
						{finding.errorOutput ? (
							<pre className="text-[10px] font-mono bg-muted/30 p-1.5 rounded text-muted-foreground overflow-x-auto max-h-24">
								{finding.errorOutput}
							</pre>
						) : null}
					</li>
				);
			})}
		</ul>
	);
}
