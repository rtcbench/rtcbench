import { expect, it } from "bun:test";
import { parse, routePath } from "./routes";
import { systemPath, systemRoute } from "./systems/routes";
import { restoreTabs } from "./tab-routes";
import { restoreLayout, panes } from "@labpane/os";

it("round-trips current URLs and rejects unsupported paths and malformed encoding", () => {
  for (const path of [
    "systems/deployments",
    "systems/deployments/new/instances/new/keys/new",
    "systems/instances/instance-id/networks/new",
    "systems/networks/new",
    "systems/networks/network-id",
    "systems/keys/key-id",
    "configs/variables/new",
    "environment/Lab%20variables",
  ]) {
    expect(routePath(parse("/" + path)!)).toBe(path);
  }
  for (const path of [
    "/servers",
    "/plugins/new/server/key",
    "/sfus/id",
    "/environments",
    "/systems/%E0%A4%A",
  ])
    expect(parse(path)).toBeNull();
});

it("restores independent Systems tabs and their pane ownership", () => {
  const tabs = restoreTabs(
    JSON.stringify([
      {
        key: "first",
        id: "systems/deployments",
        path: "systems/deployments/new/instances/new/keys/new",
      },
      {
        key: "second",
        id: "systems/deployments",
        path: "systems/deployments/deployment-id",
      },
      {
        key: "instances",
        id: "systems/instances",
        path: "systems/instances/instance-id",
      },
      { key: "unsupported", id: "servers", path: "servers" },
    ]),
  );
  expect(tabs).toEqual([
    {
      key: "first",
      id: "systems/deployments",
      path: "systems/deployments/new/instances/new/keys/new",
    },
    {
      key: "second",
      id: "systems/deployments",
      path: "systems/deployments/deployment-id",
    },
    {
      key: "instances",
      id: "systems/instances",
      path: "systems/instances/instance-id",
    },
  ]);
  const layout = {
    id: "split",
    axis: "x",
    ratio: 0.5,
    first: {
      id: "left",
      tabs: ["first", "instances"],
      active: "first",
    },
    second: { id: "right", tabs: ["second"], active: "second" },
  };
  expect(
    panes(
      restoreLayout(
        JSON.stringify(layout),
        tabs.map((t) => t.key),
      ),
    ).map((p) => p.tabs),
  ).toEqual([["first", "instances"], ["second"]]);
});

it("validates nested setup steps and returns the correct parent for edits and creation", () => {
  expect(systemRoute("deployments/new/instances/new/keys/new")?.parent).toBe(
    "systems/deployments/new/instances/new",
  );
  expect(systemRoute("instances/existing/networks/new")?.parent).toBe(
    "systems/instances/existing",
  );
  expect(systemRoute("keys/existing")?.parent).toBe("systems/keys");
  expect(systemRoute("networks/new")?.parent).toBe("systems/networks");
  expect(systemRoute("networks/existing")?.parent).toBe("systems/networks");
  for (const path of [
    "unknown",
    "instances/new/keys",
    "keys/new/instances/new",
    "deployments/existing/keys/new",
  ])
    expect(systemRoute(path)).toBeUndefined();
});

it("keeps Systems links and nested back links inside a proxy subpath", () => {
  const base = "https://example.com/app/";
  for (const path of [
    systemPath("deployments", "deployment-id"),
    systemRoute("deployments/new/instances/new/keys/new")!.parent,
    systemRoute("instances/existing")!.parent,
  ])
    expect(new URL(path, base).pathname).toBe("/app/" + path);
});
