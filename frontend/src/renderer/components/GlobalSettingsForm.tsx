import { DashboardSubhead } from "./DashboardSubhead";
import { DirectorKeySection } from "./DirectorKeySection";
import { MigrationSection } from "./MigrationSection";
import { UpdatesSection } from "./UpdatesSection";

// App-wide settings, shown from the sidebar when no project is selected. Each
// section is a self-contained card: Updates (auto-update channel, #2207),
// Director (the provider API key it needs to run — see DirectorKeySection for
// why this lives here despite the Director's engine being per-project config),
// and Migration (re-run the legacy-AO import, #2205).
export function GlobalSettingsForm() {
	return (
		<div className="flex h-full min-h-0 flex-col bg-background text-foreground">
			<DashboardSubhead title="Global settings" subtitle="Settings that apply across all projects" />
			<div className="min-h-0 flex-1 overflow-y-auto p-[18px]">
				<div className="mx-auto flex max-w-2xl flex-col gap-4">
					<UpdatesSection />
					<DirectorKeySection />
					<MigrationSection />
				</div>
			</div>
		</div>
	);
}
