import { expect, it } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import {
  docPath,
  type EnvVar,
  formatEnv,
  parse,
  parseConfig,
  parseEnv,
  validName,
} from "./api";
import { highlight } from "./yaml";

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
  expect(parse("/environments")).toEqual({
    page: "environments",
    id: undefined,
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
  expect(html).toContain('<span class="text-link">usersPerRoom</span>');
  expect(html).toContain(
    'text-violet-600 dark:text-violet-400">$env:PLUGIN_ID=livekit<',
  );
  expect(html).toContain(
    'text-violet-600 dark:text-violet-400">{{spec.network.serverIP}}<',
  );
  expect(html).toContain(
    '<span class="text-amber-700 dark:text-amber-300">5</span>',
  );
  expect(html).toContain('<span class="text-muted-foreground"># pick</span>');
  expect(html).toContain('<span class="text-link">name</span>');
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
