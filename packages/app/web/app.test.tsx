import { parse } from "./routes";
import { expect, it } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import {
  docPath,
  type EnvVar,
  formatEnv,
  parseConfig,
  parseEnv,
  validName,
} from "./api";
import { highlight } from "./yaml";
import { destinationFor, restoreTabs, visitTab } from "./tabs";

it("keeps plugin projects in independent tabs", () => {
  const route = parse("/plugins/my-plugin")!;
  expect(route).toEqual({ page: "plugins", id: "my-plugin" });
  expect(destinationFor(route)).toBe("plugins/new");
  expect(destinationFor({ page: "plugins" })).toBe("plugins");
  const tabs = visitTab([], { ...route, tabKey: "first" });
  const next = visitTab(tabs, {
    page: "plugins",
    id: "another",
    tabKey: "second",
  });
  expect(restoreTabs(JSON.stringify(next))).toEqual(next);
});

it("restores ordinary tabs, preserves an empty workspace, and rejects invalid saved routes", () => {
  expect(restoreTabs(null).map((tab) => tab.id)).toEqual(["runs", "configs"]);
  expect(restoreTabs("[]")).toEqual([]);
  expect(restoreTabs(null, true).map((tab) => tab.id)).toEqual([
    "welcome",
    "runs",
    "configs",
  ]);
  expect(restoreTabs("[]", true)).toEqual([]);
  const saved = JSON.stringify(restoreTabs(null, true));
  expect(restoreTabs(saved, true)).toEqual(restoreTabs(null, true));
  expect(restoreTabs(saved)).toEqual(restoreTabs(null, true));
  expect(restoreTabs(JSON.stringify(restoreTabs(null)), true)).toEqual(
    restoreTabs(null),
  );
  expect(destinationFor({ page: "welcome" })).toBe("welcome");
  expect(restoreTabs("invalid")).toEqual(restoreTabs(null));
  expect(
    restoreTabs(
      JSON.stringify([
        { key: "configs", id: "configs", path: "config/lab" },
        { key: "configs", id: "configs", path: "configs" },
        {
          key: "external",
          id: "systems/deployments",
          path: "https://example.com",
        },
        { key: "wrong", id: "runs", path: "systems/deployments" },
        { id: "configs", path: "configs" },
      ]),
    ),
  ).toEqual([{ key: "configs", id: "configs", path: "config/lab" }]);
  const tabs = visitTab(restoreTabs(null), {
    page: "config",
    id: "folder/lab",
  });
  expect(tabs).toEqual([
    { key: "runs", id: "runs", path: "runs" },
    { key: "configs", id: "configs", path: "configs" },
    { key: "config/folder/lab", id: "configs", path: "config/folder/lab" },
  ]);
  expect(visitTab([], { page: "workspace" })).toEqual([]);
  expect(
    destinationFor({
      page: "systems",
      id: "deployments/new/instances/new/keys/new",
    }),
  ).toBe("systems/deployments");
  expect(
    destinationFor({ page: "systems", id: "instances/new/networks/new" }),
  ).toBe("systems/instances");
  expect(destinationFor({ page: "systems", id: "networks/new" })).toBe(
    "systems/networks",
  );
  expect(destinationFor({ page: "environment", id: "Lab" })).toBe(
    "configs/variables",
  );
});

it("keeps duplicate tabs and their routes independent across restoration and history", () => {
  let tabs = visitTab(restoreTabs(null), { page: "configs", tabKey: "second" });
  tabs = visitTab(tabs, { page: "config", id: "draft", tabKey: "second" });
  expect(tabs.filter((t) => t.id === "configs").map((t) => t.path)).toEqual([
    "configs",
    "config/draft",
  ]);
  expect(restoreTabs(JSON.stringify(tabs))).toEqual(tabs);
  expect(visitTab(tabs, { page: "configs", tabKey: "configs" })).toEqual(tabs);
  expect(visitTab(tabs, { page: "runs", tabKey: "second" })).toEqual(tabs);
  expect(visitTab([], { page: "welcome", tabKey: "closed" })[0]?.key).toBe(
    "closed",
  );
  const cross = visitTab(
    tabs.filter((t) => t.id !== "runs"),
    { page: "run", id: "result", tabKey: "second" },
  );
  expect(new Set(cross.map((t) => t.key)).size).toBe(cross.length);
});

it("reads and writes environment files", () => {
  const vars: EnvVar[] = [
    { name: "PLUGIN_ID", type: "variable", value: "1.10" },
    { name: "EMPTY", type: "variable", value: "" },
    { name: "TOKEN", type: "secret" },
    { name: "SERVER_IP", type: "system" },
  ];
  expect(parseEnv(formatEnv(vars))).toEqual(vars);
  expect(parseEnv("- name: A\n  type: variable\n  value: 007\n")).toEqual([
    { name: "A", type: "variable", value: "007" },
  ]);
  expect(parseEnv("[]\n")).toEqual([]);
});

it("parses tables, detail pages and names with slashes", () => {
  expect(parse("/systems/instances/new/keys/new")).toEqual({
    page: "systems",
    id: "instances/new/keys/new",
  });
  expect(parse("/systems")).toEqual({ page: "systems", id: "deployments" });
  expect(parse("/runs")).toEqual({ page: "runs", id: undefined });
  expect(parse("/run/ce408be5-aae1-491d-895b-121d9a900e5f")).toEqual({
    page: "run",
    id: "ce408be5-aae1-491d-895b-121d9a900e5f",
  });
  expect(parse(docPath("config", "jitsi/250-viewer-bots"))).toEqual({
    page: "config",
    id: "jitsi/250-viewer-bots",
  });
  expect(parse(docPath("environment", "Our Data (lab)"))).toEqual({
    page: "environment",
    id: "Our Data (lab)",
  });
  expect(parse("/configs/variables")).toEqual({
    page: "configs",
    id: "variables",
  });
  expect(parse("/config/tutorial.yml")).toBeNull();
  expect(parse("/api/v1/runs")).toBeNull();
});

const render = (source: string) =>
  renderToStaticMarkup(<>{highlight(source)}</>);

it("keeps the source text and colors keys, strings, literals and variables", () => {
  const source = `spec:
  plugin: $env:PLUGIN_ID=livekit # pick
  usersPerRoom: 5
  _wsURL: 'ws://{{spec.network.serverIP}}:7880'
  streams:
    - name: general
`;
  const html = render(source);
  expect(html.replace(/<[^>]+>/g, "").replaceAll("&#x27;", "'")).toBe(source);
  expect(html).toContain('<span class="labui:text-link">usersPerRoom</span>');
  expect(html).toContain(
    'labui:text-violet-600 labui:dark:text-violet-400">$env:PLUGIN_ID=livekit<',
  );
  expect(html).toContain(
    'labui:text-violet-600 labui:dark:text-violet-400">{{spec.network.serverIP}}<',
  );
  expect(html).toContain('<span class="labui:text-warning">5</span>');
  expect(html).toContain(
    '<span class="labui:text-muted-foreground"># pick</span>',
  );
  expect(html).toContain('<span class="labui:text-link">name</span>');
});

it("summarizes the spec and reports YAML errors", () => {
  const { summary, errors } = parseConfig(`spec:
  plugin: $env:PLUGIN_ID=jitsi
  conference:
    totalRooms: 25
    usersPerRoom: 10
    cameras:
      perRoom: 0
  network:
    serverIP: $env:SERVER_IP
`);
  expect(errors).toEqual([]);
  expect(summary).toEqual({
    plugin: "$env:PLUGIN_ID=jitsi",
    scenario: null,
    rooms: 25,
    usersPerRoom: 10,
    camerasPerRoom: 0,
    serverIP: "$env:SERVER_IP",
  });
  expect(parseConfig("spec: [unclosed").errors.length).toBeGreaterThan(0);
});

it("accepts names that are valid folders on Mac, Linux and Windows", () => {
  for (const name of [
    "Our Data (First phase) 2-20-2027",
    "jitsi/250-viewer-bots",
    "Données #2, v1.5 [final]",
  ])
    expect(validName(name)).toBe(true);
  for (const name of [
    "",
    "a:b",
    "a\\b",
    "a*",
    "tab\there",
    "../up",
    "/root",
    "lab/",
    "lab//one",
    "ends.",
    ".hidden",
    "lab/.git",
    "lab/ padded",
    "CON",
    "lab/com1.txt",
    "a".repeat(65),
  ])
    expect(validName(name)).toBe(false);
});
