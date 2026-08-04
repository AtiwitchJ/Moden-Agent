import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import type { components } from "../../api/schema";
import { apiClient, apiErrorMessage } from "../lib/api-client";
import { DIRECTOR_KEY_ENV_VARS, WORKBOARD_DIRECTOR_HARNESS } from "../lib/workboard-config";
import { Button } from "./ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import { Label } from "./ui/label";

type Project = components["schemas"]["Project"];

const CUSTOM_PROVIDERS_ENV_VAR = "AO_DIRECTOR_CUSTOM_PROVIDERS";

const BUILT_IN_PROVIDERS = [
	{ envVar: "ANTHROPIC_API_KEY", label: "Anthropic", placeholder: "anthropic:claude-sonnet-4-6" },
	{ envVar: "OPENAI_API_KEY", label: "OpenAI", placeholder: "openai:gpt-5" },
	{ envVar: "OPENROUTER_API_KEY", label: "OpenRouter", placeholder: "openrouter:minimax/minimax-m2" },
] as const;

type CustomProviderRow = { rowId: string; id: string; baseUrl: string; apiKey: string };

function newRow(): CustomProviderRow {
	return { rowId: crypto.randomUUID(), id: "", baseUrl: "", apiKey: "" };
}

function parseCustomProviders(raw: string | undefined): CustomProviderRow[] {
	if (!raw) return [];
	try {
		const parsed: unknown = JSON.parse(raw);
		if (typeof parsed !== "object" || parsed === null) return [];
		return Object.entries(parsed as Record<string, { baseUrl?: string; apiKey?: string }>).map(([id, entry]) => ({
			rowId: crypto.randomUUID(),
			id,
			baseUrl: entry.baseUrl ?? "",
			apiKey: entry.apiKey ?? "",
		}));
	} catch {
		return [];
	}
}

const directorProjectsQueryKey = ["projects", "director-key-targets"] as const;

async function fetchDirectorProjects(): Promise<Project[]> {
	const { data, error } = await apiClient.GET("/api/v1/projects");
	if (error) throw new Error(apiErrorMessage(error));
	const ids = (data?.projects ?? []).map((p) => p.id);
	const projects = await Promise.all(
		ids.map(async (id) => {
			const { data: full, error: getError } = await apiClient.GET("/api/v1/projects/{id}", { params: { path: { id } } });
			if (getError || full?.status !== "ok") return undefined;
			return full.project as Project;
		}),
	);
	return projects.filter(
		(p): p is Project => p !== undefined && p.config?.director?.agent === WORKBOARD_DIRECTOR_HARNESS,
	);
}

// The Director's engine is still a per-project config field — a project picks
// its provider via agentConfig.model (e.g. "minimax:MiniMax-M3"). But the user
// experiences "the Director" as one thing to configure, not a per-project
// setting, so this section holds the shared registry of providers it can use —
// the three built-in ones plus any OpenAI-compatible custom endpoint (MiniMax,
// a local Ollama, ...) — and fans the same registry out to every registered
// project whose Director is active. Each project's own model field then picks
// which entry it actually uses; the rest sit unused in its env.
export function DirectorProvidersSection() {
	const queryClient = useQueryClient();
	const query = useQuery({ queryKey: directorProjectsQueryKey, queryFn: fetchDirectorProjects });
	const [builtInKeys, setBuiltInKeys] = useState<Record<string, string>>({});
	const [customProviders, setCustomProviders] = useState<CustomProviderRow[]>([]);
	const [savedAt, setSavedAt] = useState<number | null>(null);

	useEffect(() => {
		const first = query.data?.[0];
		if (!first) return;
		const env = first.config?.env ?? {};
		setBuiltInKeys(Object.fromEntries(DIRECTOR_KEY_ENV_VARS.map((name) => [name, env[name] ?? ""])));
		setCustomProviders(parseCustomProviders(env[CUSTOM_PROVIDERS_ENV_VAR]));
	}, [query.data]);

	const mutation = useMutation({
		mutationFn: async () => {
			const projects = query.data ?? [];
			const customEntries = Object.fromEntries(
				customProviders
					.filter((row) => row.id.trim() !== "" && row.baseUrl.trim() !== "")
					.map((row) => [row.id.trim(), { baseUrl: row.baseUrl.trim(), apiKey: row.apiKey.trim() || undefined }]),
			);
			const customJson = Object.keys(customEntries).length > 0 ? JSON.stringify(customEntries) : undefined;

			await Promise.all(
				projects.map(async (project) => {
					const env: Record<string, string> = { ...project.config?.env };
					for (const { envVar } of BUILT_IN_PROVIDERS) {
						const value = (builtInKeys[envVar] ?? "").trim();
						if (value) env[envVar] = value;
						else delete env[envVar];
					}
					if (customJson) env[CUSTOM_PROVIDERS_ENV_VAR] = customJson;
					else delete env[CUSTOM_PROVIDERS_ENV_VAR];

					const nextConfig = { ...project.config, env: Object.keys(env).length > 0 ? env : undefined };
					const { error } = await apiClient.PUT("/api/v1/projects/{id}/config", {
						params: { path: { id: project.id } },
						body: { config: nextConfig },
					});
					if (error) throw new Error(apiErrorMessage(error));
				}),
			);
		},
		onSuccess: () => {
			setSavedAt(Date.now());
			void queryClient.invalidateQueries({ queryKey: directorProjectsQueryKey });
		},
	});

	if (query.isLoading || !query.data || query.data.length === 0) return null;

	const updateRow = (rowId: string, patch: Partial<CustomProviderRow>) => {
		setCustomProviders((rows) => rows.map((row) => (row.rowId === rowId ? { ...row, ...patch } : row)));
		setSavedAt(null);
	};

	return (
		<Card>
			<CardHeader>
				<CardTitle className="text-[13px]">Director</CardTitle>
			</CardHeader>
			<CardContent className="flex flex-col gap-4">
				<div className="flex flex-col gap-3">
					<Label className="text-[12px] text-muted-foreground">Providers</Label>
					{BUILT_IN_PROVIDERS.map(({ envVar, label, placeholder }) => (
						<div key={envVar} className="flex items-center gap-3">
							<span className="w-24 shrink-0 text-[12px] text-foreground">{label}</span>
							<input
								aria-label={`${label} API key`}
								type="password"
								autoComplete="off"
								className="h-8 flex-1 rounded-md border border-input bg-transparent px-2.5 text-[13px] text-foreground placeholder:text-passive focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-weak"
								value={builtInKeys[envVar] ?? ""}
								onChange={(e) => {
									setBuiltInKeys((keys) => ({ ...keys, [envVar]: e.target.value }));
									setSavedAt(null);
								}}
								placeholder={`API key (used by ${placeholder})`}
							/>
						</div>
					))}
				</div>

				<div className="flex flex-col gap-3">
					<Label className="text-[12px] text-muted-foreground">Custom providers</Label>
					<p className="text-[12px] leading-5 text-muted-foreground">
						Any OpenAI-compatible endpoint — MiniMax, a local Ollama, or anything else. Set the model to{" "}
						<code>&lt;id&gt;:&lt;model-name&gt;</code> on a project to use one.
					</p>
					{customProviders.map((row) => (
						<div key={row.rowId} className="flex items-center gap-2">
							<input
								aria-label="Custom provider id"
								className="h-8 w-28 shrink-0 rounded-md border border-input bg-transparent px-2.5 text-[13px] text-foreground placeholder:text-passive focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-weak"
								value={row.id}
								onChange={(e) => updateRow(row.rowId, { id: e.target.value })}
								placeholder="minimax"
							/>
							<input
								aria-label="Custom provider base URL"
								className="h-8 flex-[2] rounded-md border border-input bg-transparent px-2.5 text-[13px] text-foreground placeholder:text-passive focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-weak"
								value={row.baseUrl}
								onChange={(e) => updateRow(row.rowId, { baseUrl: e.target.value })}
								placeholder="https://api.minimax.io/v1"
							/>
							<input
								aria-label="Custom provider API key"
								type="password"
								autoComplete="off"
								className="h-8 flex-1 rounded-md border border-input bg-transparent px-2.5 text-[13px] text-foreground placeholder:text-passive focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-weak"
								value={row.apiKey}
								onChange={(e) => updateRow(row.rowId, { apiKey: e.target.value })}
								placeholder="optional — e.g. local Ollama"
							/>
							<Button
								aria-label="Remove custom provider"
								onClick={() => {
									setCustomProviders((rows) => rows.filter((r) => r.rowId !== row.rowId));
									setSavedAt(null);
								}}
								size="icon-sm"
								type="button"
								variant="ghost"
							>
								×
							</Button>
						</div>
					))}
					<Button
						className="self-start"
						onClick={() => setCustomProviders((rows) => [...rows, newRow()])}
						size="sm"
						type="button"
						variant="outline"
					>
						+ Add custom provider
					</Button>
				</div>

				<div className="flex items-center gap-3">
					<Button disabled={mutation.isPending} onClick={() => mutation.mutate()} size="sm" type="button">
						{mutation.isPending ? "Saving…" : "Save"}
					</Button>
					{savedAt && !mutation.isPending && !mutation.isError && <span className="text-[12px] text-success">Saved.</span>}
					{mutation.isError && (
						<span className="text-[12px] text-error">
							{mutation.error instanceof Error ? mutation.error.message : "Could not save."}
						</span>
					)}
				</div>
				<p className="text-[12px] leading-5 text-muted-foreground">
					Applied to every project running the Director. Each project's own model field picks which provider it uses.
				</p>
			</CardContent>
		</Card>
	);
}
