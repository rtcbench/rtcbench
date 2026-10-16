import { expect, it } from "bun:test";
import { pluginFiles } from "./plugin-template";
import { pluginPrompt } from "./plugin-prompt";
import { parse } from "./routes";
import { restoreTabs } from "./tabs";

it("includes the project location and server download URL in the prompt", () => {
  const url = "https://bench.example/app/api/v1/plugins/my-plugin/download";
  const prompt = pluginPrompt(
    "my-plugin",
    "/srv/rtcbench/plugins/my-plugin",
    url,
  );
  expect(prompt).toContain("https://github.com/rtcbench/rtcbench");
  expect(prompt).toContain("RTCBench server: https://bench.example/app/");
  expect(prompt).toContain(
    'Project location on that server: "/srv/rtcbench/plugins/my-plugin"',
  );
  expect(prompt).toContain(`Project download: ${url}`);
  expect(prompt).toContain("may be remote");
  expect(
    pluginPrompt(
      "local",
      "C:\\data\\plugins\\local",
      "http://127.0.0.1:8080/api/v1/plugins/local/download",
    ),
  ).toContain("RTCBench server: http://127.0.0.1:8080/");
});

it("restores terminal targets and rejects obsolete session routes", () => {
  expect(parse("/terminal/old-session")).toBeNull();
  expect(
    restoreTabs(
      JSON.stringify([
        { key: "old-session", id: "terminal", path: "terminal/old-session" },
        { key: "terminal", id: "terminal", path: "terminal/plugins/my-plugin" },
        { key: "project", id: "plugins/new", path: "plugins/my-plugin" },
      ]),
    ),
  ).toEqual([
    { key: "terminal", id: "terminal", path: "terminal/plugins/my-plugin" },
    { key: "project", id: "plugins/new", path: "plugins/my-plugin" },
  ]);
});

it("generates the plugin project in App", () => {
  const files = pluginFiles("my-plugin", "example.com/my-plugin");
  expect(files["plugin.yml"]).toContain("id: my-plugin");
  expect(files["plugin/plugin.go"]).toContain("type Plugin struct{}");
  expect(files["go.mod"]).toContain("module example.com/my-plugin");
  expect(files["Dockerfile"]).toBeTruthy();
  expect(files["Makefile"]).toContain("go build -trimpath");
  expect(JSON.parse(files["web/package.json"]!).dependencies).toEqual({
    react: "^19.3.0",
    "react-dom": "^19.3.0",
  });
  for (const source of Object.values(files)) {
    expect(source).not.toContain("__PLUGIN__");
    expect(source).not.toContain("__MODULE__");
  }
});
