import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { getMock, putMock } = vi.hoisted(() => ({ getMock: vi.fn(), putMock: vi.fn() }));

vi.mock("../lib/api-client", () => ({
	apiClient: { GET: getMock, PUT: putMock },
	apiErrorMessage: (e: unknown, fb = "Request failed") =>
		e instanceof Error ? e.message : ((e as { message?: string })?.message ?? fb),
}));

import { DirectorKeySection } from "./DirectorKeySection";

type Fixture = { id: string; config: Record<string, unknown> };

// Mirrors the real two-call shape DirectorKeySection uses: list ids via
// GET /api/v1/projects, then fetch each one's full config via
// GET /api/v1/projects/{id}.
function routeProjects(projects: Fixture[]) {
	getMock.mockImplementation(async (path: string, opts?: { params?: { path?: { id?: string } } }) => {
		if (path === "/api/v1/projects") {
			return { data: { projects: projects.map((p) => ({ id: p.id })) }, error: undefined };
		}
		if (path === "/api/v1/projects/{id}") {
			const id = opts?.params?.path?.id;
			const project = projects.find((p) => p.id === id);
			return { data: project ? { status: "ok", project } : { status: "error" }, error: undefined };
		}
		throw new Error(`unmocked GET ${path}`);
	});
}

function renderSection() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
	render(
		<QueryClientProvider client={qc}>
			<DirectorKeySection />
		</QueryClientProvider>,
	);
}

beforeEach(() => {
	getMock.mockReset();
	putMock.mockReset();
	putMock.mockResolvedValue({ data: { project: {} }, error: undefined });
});

describe("DirectorKeySection", () => {
	it("renders nothing when no registered project runs the Director", async () => {
		routeProjects([{ id: "p1", config: { orchestrator: { agent: "hermes" } } }]);
		renderSection();
		await waitFor(() => expect(getMock).toHaveBeenCalled());
		expect(screen.queryByLabelText(/api key/i)).not.toBeInTheDocument();
	});

	it("saves under the env var the project's configured provider needs", async () => {
		routeProjects([
			{ id: "p1", config: { director: { agent: "director" }, agentConfig: { model: "anthropic:claude-sonnet-4-6" } } },
		]);
		renderSection();

		const input = await screen.findByLabelText(/api key/i);
		await userEvent.type(input, "sk-ant-test");
		await userEvent.click(screen.getByRole("button", { name: /save/i }));

		await waitFor(() => expect(putMock).toHaveBeenCalled());
		const body = putMock.mock.calls[0][1].body;
		expect(body.config.env).toMatchObject({ ANTHROPIC_API_KEY: "sk-ant-test" });
	});

	it("fans the same key out to every project running the Director, each under its own provider var", async () => {
		routeProjects([
			{ id: "p1", config: { director: { agent: "director" }, agentConfig: { model: "anthropic:claude-sonnet-4-6" } } },
			{ id: "p2", config: { director: { agent: "director" }, agentConfig: { model: "openrouter:minimax/minimax-m2" } } },
		]);
		renderSection();

		const input = await screen.findByLabelText(/api key/i);
		await userEvent.type(input, "shared-key");
		await userEvent.click(screen.getByRole("button", { name: /save/i }));

		await waitFor(() => expect(putMock).toHaveBeenCalledTimes(2));
		const bodies = putMock.mock.calls.map((call) => call[1].body);
		expect(bodies).toContainEqual(expect.objectContaining({ config: expect.objectContaining({ env: { ANTHROPIC_API_KEY: "shared-key" } }) }));
		expect(bodies).toContainEqual(expect.objectContaining({ config: expect.objectContaining({ env: { OPENROUTER_API_KEY: "shared-key" } }) }));
	});

	it("skips a project that does not have the Director active", async () => {
		routeProjects([
			{ id: "p1", config: { director: { agent: "director" }, agentConfig: {} } },
			{ id: "p2", config: { orchestrator: { agent: "hermes" } } },
		]);
		renderSection();

		const input = await screen.findByLabelText(/api key/i);
		await userEvent.type(input, "sk-ant-test");
		await userEvent.click(screen.getByRole("button", { name: /save/i }));

		await waitFor(() => expect(putMock).toHaveBeenCalledTimes(1));
		expect(putMock.mock.calls[0][1].params.path.id).toBe("p1");
	});

	it("prefills from the first Director project's existing key", async () => {
		routeProjects([
			{
				id: "p1",
				config: {
					director: { agent: "director" },
					agentConfig: { model: "anthropic:claude-sonnet-4-6" },
					env: { ANTHROPIC_API_KEY: "sk-ant-existing" },
				},
			},
		]);
		renderSection();

		const input = await screen.findByLabelText(/api key/i);
		await waitFor(() => expect(input).toHaveValue("sk-ant-existing"));
	});
});
