import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { getMock, putMock } = vi.hoisted(() => ({ getMock: vi.fn(), putMock: vi.fn() }));

vi.mock("../lib/api-client", () => ({
	apiClient: { GET: getMock, PUT: putMock },
	apiErrorMessage: (e: unknown, fb = "Request failed") =>
		e instanceof Error ? e.message : ((e as { message?: string })?.message ?? fb),
}));

import { DirectorProvidersSection } from "./DirectorProvidersSection";

type Fixture = { id: string; config: Record<string, unknown> };

// Mirrors the real two-call shape DirectorProvidersSection uses: list ids via
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
			<DirectorProvidersSection />
		</QueryClientProvider>,
	);
}

function customProviderRows() {
	return screen.getAllByLabelText("Custom provider id").map((idInput) => idInput.closest("div") as HTMLElement);
}

beforeEach(() => {
	getMock.mockReset();
	putMock.mockReset();
	putMock.mockResolvedValue({ data: { project: {} }, error: undefined });
});

describe("DirectorProvidersSection", () => {
	it("renders nothing when no registered project runs the Director", async () => {
		routeProjects([{ id: "p1", config: { orchestrator: { agent: "hermes" } } }]);
		renderSection();
		await waitFor(() => expect(getMock).toHaveBeenCalled());
		expect(screen.queryByText("Director")).not.toBeInTheDocument();
	});

	it("saves each built-in provider key under its own env var", async () => {
		routeProjects([{ id: "p1", config: { director: { agent: "director" } } }]);
		renderSection();

		await userEvent.type(await screen.findByLabelText("Anthropic API key"), "sk-ant-test");
		await userEvent.type(screen.getByLabelText("OpenAI API key"), "sk-oai-test");
		await userEvent.click(screen.getByRole("button", { name: /^save$/i }));

		await waitFor(() => expect(putMock).toHaveBeenCalled());
		const body = putMock.mock.calls[0][1].body;
		expect(body.config.env).toMatchObject({ ANTHROPIC_API_KEY: "sk-ant-test", OPENAI_API_KEY: "sk-oai-test" });
		expect(body.config.env.OPENROUTER_API_KEY).toBeUndefined();
	});

	it("adds a custom provider row and saves it as a JSON registry", async () => {
		routeProjects([{ id: "p1", config: { director: { agent: "director" } } }]);
		renderSection();

		await screen.findByLabelText("Anthropic API key");
		await userEvent.click(screen.getByRole("button", { name: /add custom provider/i }));

		await userEvent.type(screen.getByLabelText("Custom provider id"), "minimax");
		await userEvent.type(screen.getByLabelText("Custom provider base URL"), "https://api.minimax.io/v1");
		await userEvent.type(screen.getByLabelText("Custom provider API key"), "sk-cp-test");
		await userEvent.click(screen.getByRole("button", { name: /^save$/i }));

		await waitFor(() => expect(putMock).toHaveBeenCalled());
		const env = putMock.mock.calls[0][1].body.config.env;
		expect(JSON.parse(env.AO_DIRECTOR_CUSTOM_PROVIDERS)).toEqual({
			minimax: { baseUrl: "https://api.minimax.io/v1", apiKey: "sk-cp-test" },
		});
	});

	it("saves a custom provider with no api key, e.g. a local Ollama endpoint", async () => {
		routeProjects([{ id: "p1", config: { director: { agent: "director" } } }]);
		renderSection();

		await screen.findByLabelText("Anthropic API key");
		await userEvent.click(screen.getByRole("button", { name: /add custom provider/i }));
		await userEvent.type(screen.getByLabelText("Custom provider id"), "ollama");
		await userEvent.type(screen.getByLabelText("Custom provider base URL"), "http://localhost:11434/v1");
		await userEvent.click(screen.getByRole("button", { name: /^save$/i }));

		await waitFor(() => expect(putMock).toHaveBeenCalled());
		const env = putMock.mock.calls[0][1].body.config.env;
		expect(JSON.parse(env.AO_DIRECTOR_CUSTOM_PROVIDERS)).toEqual({
			ollama: { baseUrl: "http://localhost:11434/v1" },
		});
	});

	it("drops a custom provider row left without an id or base URL", async () => {
		routeProjects([{ id: "p1", config: { director: { agent: "director" } } }]);
		renderSection();

		await screen.findByLabelText("Anthropic API key");
		await userEvent.click(screen.getByRole("button", { name: /add custom provider/i }));
		await userEvent.click(screen.getByRole("button", { name: /^save$/i }));

		await waitFor(() => expect(putMock).toHaveBeenCalled());
		expect(putMock.mock.calls[0][1].body.config.env).toBeUndefined();
	});

	it("removes a custom provider row", async () => {
		routeProjects([{ id: "p1", config: { director: { agent: "director" } } }]);
		renderSection();

		await screen.findByLabelText("Anthropic API key");
		await userEvent.click(screen.getByRole("button", { name: /add custom provider/i }));
		await userEvent.click(screen.getByRole("button", { name: /add custom provider/i }));
		const rows = customProviderRows();
		expect(rows).toHaveLength(2);

		await userEvent.type(within(rows[0]).getByLabelText("Custom provider id"), "keep");
		await userEvent.type(within(rows[0]).getByLabelText("Custom provider base URL"), "https://keep.example/v1");
		await userEvent.type(within(rows[1]).getByLabelText("Custom provider id"), "drop");
		await userEvent.type(within(rows[1]).getByLabelText("Custom provider base URL"), "https://drop.example/v1");

		await userEvent.click(within(rows[1]).getByRole("button", { name: /remove custom provider/i }));
		expect(customProviderRows()).toHaveLength(1);

		await userEvent.click(screen.getByRole("button", { name: /^save$/i }));
		await waitFor(() => expect(putMock).toHaveBeenCalled());
		const env = putMock.mock.calls[0][1].body.config.env;
		expect(Object.keys(JSON.parse(env.AO_DIRECTOR_CUSTOM_PROVIDERS))).toEqual(["keep"]);
	});

	it("fans the same provider registry out identically to every project running the Director", async () => {
		routeProjects([
			{ id: "p1", config: { director: { agent: "director" }, agentConfig: { model: "anthropic:claude-sonnet-4-6" } } },
			{ id: "p2", config: { director: { agent: "director" }, agentConfig: { model: "openrouter:minimax/minimax-m2" } } },
		]);
		renderSection();

		await userEvent.type(await screen.findByLabelText("Anthropic API key"), "shared-key");
		await userEvent.click(screen.getByRole("button", { name: /^save$/i }));

		await waitFor(() => expect(putMock).toHaveBeenCalledTimes(2));
		const bodies = putMock.mock.calls.map((call) => call[1].body);
		expect(bodies[0].config.env.ANTHROPIC_API_KEY).toBe("shared-key");
		expect(bodies[1].config.env.ANTHROPIC_API_KEY).toBe("shared-key");
	});

	it("skips a project that does not have the Director active", async () => {
		routeProjects([
			{ id: "p1", config: { director: { agent: "director" } } },
			{ id: "p2", config: { orchestrator: { agent: "hermes" } } },
		]);
		renderSection();

		await userEvent.type(await screen.findByLabelText("Anthropic API key"), "sk-ant-test");
		await userEvent.click(screen.getByRole("button", { name: /^save$/i }));

		await waitFor(() => expect(putMock).toHaveBeenCalledTimes(1));
		expect(putMock.mock.calls[0][1].params.path.id).toBe("p1");
	});

	it("prefills built-in keys and custom providers from the first Director project's existing config", async () => {
		routeProjects([
			{
				id: "p1",
				config: {
					director: { agent: "director" },
					env: {
						ANTHROPIC_API_KEY: "sk-ant-existing",
						AO_DIRECTOR_CUSTOM_PROVIDERS: JSON.stringify({ minimax: { baseUrl: "https://api.minimax.io/v1", apiKey: "sk-cp-existing" } }),
					},
				},
			},
		]);
		renderSection();

		await waitFor(async () => expect(await screen.findByLabelText("Anthropic API key")).toHaveValue("sk-ant-existing"));
		expect(screen.getByLabelText("Custom provider id")).toHaveValue("minimax");
		expect(screen.getByLabelText("Custom provider base URL")).toHaveValue("https://api.minimax.io/v1");
		expect(screen.getByLabelText("Custom provider API key")).toHaveValue("sk-cp-existing");
	});
});
