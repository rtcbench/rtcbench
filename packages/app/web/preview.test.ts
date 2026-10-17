import { afterAll, beforeAll, expect, it } from "bun:test";
import { readFile } from "node:fs/promises";
import { createPreview, readOnly } from "./preview";
import type { Doc, Run, RunFile } from "./api";

const base = new URL("https://preview.test/site/");
const examples = ["churn.yml", "jitsi/250-viewer-bots.yml"];
const realFetch = globalThis.fetch;

beforeAll(() => {
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    const url = new URL(input instanceof Request ? input.url : String(input));
    if (url.pathname === "/site/examples/index.json")
      return Response.json(examples);
    const file = url.pathname.replace("/site/examples/", "");
    return new Response(
      await readFile(
        new URL(`../../../config-examples/${file}`, import.meta.url),
        "utf8",
      ),
    );
  }) as typeof fetch;
});
afterAll(() => {
  globalThis.fetch = realFetch;
});

const api = (path: string) => `${base}api/v1${path}`;

it("serves sample runs with summaries, viewers, files and logs", async () => {
  const preview = createPreview(base);
  const runs: Run[] = await (await preview(api("/runs"))).json();
  expect(runs.map((run) => run.status)).toEqual([
    "completed",
    "completed",
    "failed",
    "completed",
    "interrupted",
  ]);
  const run: Run = await (await preview(api(`/runs/${runs[0]!.id}`))).json();
  expect(run.summary?.Receivers).toHaveLength(250);
  expect(run.summary?.Operations?.[0]?.Operation).toBe("join");
  const viewers = await (await preview(api(`/runs/${run.id}/viewers`))).json();
  expect(viewers.viewers_total).toBe(250);
  const files: RunFile[] = await (
    await preview(api(`/runs/${run.id}/files`))
  ).json();
  expect(files.map((file) => file.path)).toContain("logs/combined.log");
  const config = await (
    await preview(api(`/runs/${run.id}/files/config.yml`))
  ).text();
  expect(config).toContain("plugin: jitsi");
  const log = await (await preview(api(`/runs/${run.id}/logs/general`))).text();
  expect(log).toContain("Setup() ok");
  expect((await preview(api(`/runs/${run.id}/logs/nope`))).status).toBe(404);
});

it("loads configs from the example files and keeps edits in memory", async () => {
  const preview = createPreview(base);
  const list = async (): Promise<Doc[]> =>
    (await preview(api("/configs"))).json();
  expect((await list()).map((doc) => doc.name)).toEqual([
    "churn",
    "jitsi/250-viewer-bots",
  ]);
  const post = (body: unknown) =>
    preview(api("/configs"), { method: "POST", body: JSON.stringify(body) });
  expect((await post({ name: "mine", yaml: "a: 1\n" })).status).toBe(201);
  expect((await post({ name: "mine", yaml: "" })).status).toBe(409);
  const put = await preview(api("/configs/mine"), {
    method: "PUT",
    body: JSON.stringify({ name: "ours", yaml: "a: 2\n" }),
  });
  expect(((await put.json()) as Doc).name).toBe("ours");
  expect((await list()).map((doc) => doc.name)).toContain("ours");
  expect(
    (await preview(api("/configs/ours"), { method: "DELETE" })).status,
  ).toBe(204);
  expect((await preview(api("/configs/ours"))).status).toBe(404);
  const environments: Doc[] = await (
    await preview(api("/environments"))
  ).json();
  expect(environments.map((doc) => doc.name)).toEqual(["cloud", "lab"]);
});

it("answers the other pages and refuses writes", async () => {
  const preview = createPreview(base);
  expect(await (await preview(api("/version"))).json()).toEqual({
    version: "preview",
  });
  const inventory = await (await preview(api("/systems"))).json();
  expect(inventory.deployments).toHaveLength(2);
  expect(inventory.plugins.map((p: { id: string }) => p.id)).toEqual([
    "livekit",
    "janus",
    "mediasoup",
    "jitsi",
  ]);
  const benchmarks = await (await preview(api("/benchmarks"))).json();
  expect(Object.keys(benchmarks[0].report.results)).toEqual([
    "livekit/rtcbench",
    "livekit/chromium",
  ]);
  expect((await preview(api("/plugins"))).status).toBe(200);
  const refused = await preview(api("/runs"), { method: "POST", body: "{}" });
  expect(refused.status).toBe(503);
  expect((await refused.json()).error).toBe(readOnly);
  expect((await preview(api("/terminal?kind=server"))).status).toBe(503);
  expect((await preview(api("/unknown"))).status).toBe(404);
});
