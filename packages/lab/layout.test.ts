import { expect, it } from "bun:test";
import {
  dockTab,
  dropRegion,
  isSplit,
  layoutBoxes,
  minimumSize,
  panes,
  reconcileLayout,
  restoreLayout,
  updateLayout,
  type Layout,
} from "./layout";
const initial: Layout = { id: "primary", tabs: ["a", "b", "c"], active: "a" };
it("inserts a moved tab at the chosen position without changing its neighbors", () => {
  const split = dockTab(initial, "c", "primary", "bottom");
  for (const [index, expected] of [
    [0, ["c", "a", "b"]],
    [1, ["a", "c", "b"]],
    [2, ["a", "b", "c"]],
  ] as const) {
    const result = panes(dockTab(split, "c", "primary", undefined, index));
    expect(result).toEqual([
      { id: "primary", tabs: [...expected], active: "c" },
    ]);
  }
});
it("opens a new bottom pane without changing its owner's active content", () => {
  const next = reconcileLayout(
    initial,
    ["a", "b", "c", "terminal"],
    "a",
    "primary",
  );
  const layout = dockTab(next, "terminal", "primary", "bottom");
  expect(panes(layout).map((p) => p.active)).toEqual(["a", "terminal"]);
  expect(panes(layout).map((p) => p.tabs)).toEqual([
    ["a", "b", "c"],
    ["terminal"],
  ]);
});
it("moves tabs through nested splits and collapses emptied panes without duplicating tabs", () => {
  let layout = dockTab(initial, "b", "primary", "right");
  expect(isSplit(layout) && layout.ratio).toBe(0.5);
  const right = panes(layout)[1]!;
  layout = dockTab(layout, "c", right.id, "bottom");
  expect(panes(layout).map((p) => p.tabs)).toEqual([["a"], ["b"], ["c"]]);
  expect(isSplit(layout) && isSplit(layout.second) && layout.second.ratio).toBe(
    2 / 3,
  );
  layout = dockTab(layout, "c", right.id);
  expect(panes(layout).map((p) => p.tabs)).toEqual([["a"], ["b", "c"]]);
  layout = dockTab(layout, "a", right.id);
  expect(isSplit(layout)).toBe(false);
  expect(panes(layout)[0]!.tabs).toEqual(["b", "c", "a"]);
  expect(dockTab(layout, "missing", right.id, "left")).toBe(layout);
});
it("keeps independent selections, opens tabs in the requested pane, and survives malformed storage", () => {
  const split = dockTab(initial, "b", "primary", "right"),
    right = panes(split)[1]!;
  const opened = reconcileLayout(split, ["a", "b", "c", "d"], "d", right.id);
  expect(panes(opened).map((p) => p.active)).toEqual(["a", "d"]);
  expect(restoreLayout(JSON.stringify(opened), ["a", "b", "c", "d"])).toEqual(
    opened,
  );
  expect(reconcileLayout(opened, ["a", "b", "c", "d"], "d")).toBe(opened);
  const empty = reconcileLayout(opened, []);
  expect(reconcileLayout(empty, [])).toBe(empty);
  expect(
    panes(restoreLayout('{"id":"p","axis":"x","ratio":null}', ["a"])),
  ).toEqual([{ id: "primary", tabs: ["a"], active: "a" }]);
  const broken = {
    id: "root",
    axis: "x",
    ratio: 0.5,
    first: { id: "p", tabs: ["a", "a", "gone"] },
    second: { id: "q", tabs: ["a", "b"] },
  };
  expect(
    panes(restoreLayout(JSON.stringify(broken), ["a", "b", "c"])).flatMap(
      (p) => p.tabs,
    ),
  ).toEqual(["a", "c", "b"]);
});
it("keeps every nested pane above minimum size and dividers inside their bounds", () => {
  let layout = dockTab(initial, "b", "primary", "right");
  layout = dockTab(layout, "c", panes(layout)[1]!.id, "bottom");
  layout = updateLayout(layout, layout.id, (node) => ({
    ...node,
    ratio: 0.99,
  }));
  for (const gap of [
    { x: 4, y: 2 },
    { x: 6, y: 4 },
    { x: 7.15, y: 6.3 },
    { x: 8, y: 8 },
  ]) {
    expect(minimumSize(layout, gap)).toEqual({
      x: 560 + gap.x,
      y: 360 + gap.y,
    });
    for (const [width, height] of [
      [200, 100],
      [1200, 800],
    ]) {
      const { leaves, dividers } = layoutBoxes(layout, width!, height!, gap);
      for (const leaf of leaves) {
        expect(leaf.width).toBeGreaterThanOrEqual(280 - 1e-9);
        expect(leaf.height).toBeGreaterThanOrEqual(180 - 1e-9);
      }
      for (const divider of dividers) {
        expect(divider.ratio).toBeGreaterThanOrEqual(divider.low);
        expect(divider.ratio).toBeLessThanOrEqual(divider.high);
        expect(
          divider.split.axis === "x" ? divider.width : divider.height,
        ).toBe(gap[divider.split.axis]);
      }
    }
  }
});

it("previews the same constrained bottom region that a small pane will receive", () => {
  const layout = dockTab(initial, "b", "primary", "bottom");
  for (const gap of [
    { x: 4, y: 2 },
    { x: 6, y: 4 },
    { x: 7.15, y: 6.3 },
    { x: 8, y: 8 },
  ]) {
    const actual = layoutBoxes(layout, 800, 400, gap).leaves.find((p) =>
      p.pane.tabs.includes("b"),
    )!;
    const preview = dropRegion(
      { x: 0, y: 0, width: 800, height: 400 },
      "bottom",
      gap,
    );
    for (const key of ["x", "y", "width", "height"] as const)
      expect(actual[key]).toBeCloseTo(preview[key], 9);
  }
});
