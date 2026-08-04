import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import type { components } from "../../api/schema";
import { apiClient, apiErrorMessage } from "../lib/api-client";
import { DIRECTOR_KEY_ENV_VARS, directorKeyEnvVar, WORKBOARD_DIRECTOR_HARNESS } from "../lib/workboard-config";
import { Button } from "./ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import { Label } from "./ui/label";

type Project = components["schemas"]["Project"];

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

// The Director's engine (and so the provider key it needs) is still a
// per-project config field — different projects can run different models. But
// the user experiences "the Director" as one thing, not a per-project setting,
// so this section takes a single key and fans it out: for every registered
// project whose Director is active, it writes the key under that project's own
// configured provider's env var.
export function DirectorKeySection() {
	const queryClient = useQueryClient();
	const query = useQuery({ queryKey: directorProjectsQueryKey, queryFn: fetchDirectorProjects });
	const [key, setKey] = useState("");
	const [savedAt, setSavedAt] = useState<number | null>(null);

	useEffect(() => {
		const first = query.data?.[0];
		if (!first) return;
		const existing = first.config?.env?.[directorKeyEnvVar(first.config?.agentConfig?.model ?? "")] ?? "";
		setKey(existing);
	}, [query.data]);

	const keyVar = directorKeyEnvVar(query.data?.[0]?.config?.agentConfig?.model ?? "");

	const mutation = useMutation({
		mutationFn: async () => {
			const trimmed = key.trim();
			const projects = query.data ?? [];
			await Promise.all(
				projects.map(async (project) => {
					const varName = directorKeyEnvVar(project.config?.agentConfig?.model ?? "");
					const env: Record<string, string> = { ...project.config?.env };
					for (const name of DIRECTOR_KEY_ENV_VARS) delete env[name];
					if (trimmed) env[varName] = trimmed;
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

	return (
		<Card>
			<CardHeader>
				<CardTitle className="text-[13px]">Director</CardTitle>
			</CardHeader>
			<CardContent className="flex flex-col gap-3">
				<div className="flex flex-col gap-1.5">
					<Label htmlFor="directorKey" className="text-[12px] text-muted-foreground">
						API key
					</Label>
					<input
						id="directorKey"
						type="password"
						autoComplete="off"
						className="h-8 w-full rounded-md border border-input bg-transparent px-2.5 text-[13px] text-foreground placeholder:text-passive focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-weak"
						value={key}
						onChange={(e) => {
							setKey(e.target.value);
							setSavedAt(null);
						}}
						placeholder={keyVar}
					/>
					<p className="text-[12px] leading-5 text-muted-foreground">
						Applied to every project running the Director, each under its own configured engine&apos;s key (
						<code>{keyVar}</code> for this one). The Director will not start without it.
					</p>
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
			</CardContent>
		</Card>
	);
}
