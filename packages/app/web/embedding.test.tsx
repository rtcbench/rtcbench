import { expect, it } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { build } from "esbuild";
import {
  ConfigPage,
  ConfigsPage,
  RunPage,
  RunsPage,
  SystemsPage,
  VariablePage,
  VariablesPage,
  SystemOverridesPage,
} from "../pages";
import { page, useNavigation } from "./integration";
import { useApi } from "./api";

it("keeps API settings isolated between embedded pages", async () => {
  const clients: ReturnType<typeof useApi>[] = [];
  const calls: [string, RequestInit | undefined][] = [];
  const fetcher = (async (url, init) => {
    calls.push([String(url), init]);
    return Response.json({ name: "saved" });
  }) as typeof fetch;
  const Probe = page(() => {
    clients.push(useApi());
    return null;
  });
  renderToStaticMarkup(
    <>
      <Probe apiUrl="https://controller.example/api/v1/" fetch={fetcher} />
      <Probe apiUrl="/other/api/v1" fetch={fetcher} />
    </>,
  );
  await clients[0]!.request("POST", "/configs", { name: "first" });
  await clients[1]!.request("GET", "/runs");
  expect(calls.map(([url]) => url)).toEqual([
    "https://controller.example/api/v1/configs",
    "/other/api/v1/runs",
  ]);
  expect(calls[0]![1]?.body).toBe('{"name":"first"}');
});

it("renders pages in a host layout without a provider, Lab, or browser globals", () => {
  for (const content of [
    <RunsPage />,
    <RunPage id="run-id" />,
    <ConfigsPage />,
    <ConfigPage />,
    <VariablesPage />,
    <VariablePage />,
    <SystemOverridesPage />,
    <SystemsPage />,
  ]) {
    const html = renderToStaticMarkup(
      <section aria-label="Host layout">{content}</section>,
    );
    expect(html).toContain('aria-label="Host layout"');
    expect(html).not.toContain('role="tablist"');
  }
  const html = renderToStaticMarkup(
    <ConfigPage href={(path) => `/tools/rtc/${path}`} />,
  );
  expect(html).toContain('href="/tools/rtc/configs"');
});

it("lets the host router handle navigation while preserving modified clicks", () => {
  let navigation!: ReturnType<typeof useNavigation>;
  const Probe = page(() => {
    navigation = useNavigation();
    return null;
  });
  const visits: string[] = [];
  renderToStaticMarkup(
    <Probe
      href={(path) => `#/bench/${path}`}
      navigate={(path) => visits.push(path)}
    />,
  );
  navigation.go("/configs/new");
  expect(visits).toEqual(["configs/new"]);
  const link = navigation.link("/config/folder/name");
  expect(link.href).toBe("#/bench/config/folder/name");
  let prevented = false;
  const event = {
    button: 0,
    preventDefault: () => {
      prevented = true;
    },
    currentTarget: { target: "", hasAttribute: () => false },
  };
  type Click = Parameters<NonNullable<typeof link.onClick>>[0];
  for (const modifier of [
    "metaKey",
    "ctrlKey",
    "shiftKey",
    "altKey",
    "defaultPrevented",
  ])
    link.onClick!({ ...event, [modifier]: true } as unknown as Click);
  link.onClick!({ ...event, button: 1 } as unknown as Click);
  expect(visits).toHaveLength(1);
  expect(prevented).toBe(false);
  link.onClick!(event as unknown as Click);
  expect(visits.at(-1)).toBe("config/folder/name");
  expect(prevented).toBe(true);
});

it("keeps Lab and the standalone entry out of the pages bundle", async () => {
  const result = await build({
    entryPoints: ["pages.ts"],
    bundle: true,
    packages: "external",
    format: "esm",
    write: false,
    metafile: true,
  });
  expect(
    Object.keys(result.metafile!.inputs).some((path) =>
      /\/(app|main)\.tsx$/.test(path),
    ),
  ).toBe(false);
  const imports = Object.values(result.metafile!.outputs).flatMap((output) =>
    output.imports.map((entry) => entry.path),
  );
  expect(imports.some((path) => /^@rtcbench\/lab(?:\/|$)/.test(path))).toBe(
    false,
  );
});
