export type Pane = { id: string; tabs: string[]; active?: string };
export type Split = {
  id: string;
  axis: "x" | "y";
  ratio: number;
  first: Layout;
  second: Layout;
};
export type Layout = Pane | Split;
export type Edge = "left" | "right" | "bottom";
export type Box = { x: number; y: number; width: number; height: number };
type PaneBox = Box & { pane: Pane };
const gutter = { x: 8, y: 8 };
export const minimum = { x: 280, y: 180 };
export const isSplit = (node: Layout): node is Split => "axis" in node;
export const panes = (node: Layout): Pane[] =>
  isSplit(node) ? [...panes(node.first), ...panes(node.second)] : [node];
const clamp = (n: number, low: number, high: number) =>
  Math.max(low, Math.min(high, n));

export function updateLayout(
  node: Layout,
  id: string,
  change: (node: Layout) => Layout,
): Layout {
  if (node.id === id) return change(node);
  if (!isSplit(node)) return node;
  const first = updateLayout(node.first, id, change),
    second = updateLayout(node.second, id, change);
  return first === node.first && second === node.second
    ? node
    : { ...node, first, second };
}

export function reconcileLayout(
  node: Layout,
  keys: string[],
  active?: string,
  preferred?: string,
): Layout {
  if (!keys.length)
    return !isSplit(node) && !node.tabs.length && !node.active
      ? node
      : { id: "primary", tabs: [] };
  const seen = new Set<string>();
  const clean = (node: Layout): Layout | undefined => {
    if (isSplit(node)) {
      const first = clean(node.first),
        second = clean(node.second);
      return first && second
        ? first === node.first && second === node.second
          ? node
          : { ...node, first, second }
        : (first ?? second);
    }
    const tabs = node.tabs.filter(
      (key) => keys.includes(key) && !seen.has(key) && !!seen.add(key),
    );
    if (!tabs.length) return undefined;
    const selected = tabs.includes(active ?? "")
      ? active
      : tabs.includes(node.active ?? "")
        ? node.active
        : tabs[0];
    return tabs.length === node.tabs.length && selected === node.active
      ? node
      : { ...node, tabs, active: selected };
  };
  let next = clean(node) ?? { id: "primary", tabs: [] };
  const missing = keys.filter((key) => !seen.has(key));
  if (missing.length) {
    const leaves = panes(next),
      target = leaves.find((p) => p.id === preferred) ?? leaves[0]!;
    next = updateLayout(next, target.id, (node) => ({
      ...(node as Pane),
      tabs: [...target.tabs, ...missing],
      active: missing.includes(active ?? "")
        ? active
        : (target.active ?? missing[0]),
    }));
  }
  return next;
}

export function restoreLayout(value: string | null, keys: string[]): Layout {
  const ids = new Set<string>();
  function read(value: unknown, depth = 0): Layout {
    const n = value as Partial<Pane & Split>;
    if (!n || depth > 12 || typeof n.id !== "string" || !n.id || ids.has(n.id))
      throw Error("Invalid layout");
    ids.add(n.id);
    if (n.axis === "x" || n.axis === "y") {
      if (typeof n.ratio !== "number" || !Number.isFinite(n.ratio))
        throw Error("Invalid ratio");
      return {
        id: n.id,
        axis: n.axis,
        ratio: clamp(n.ratio, 0.05, 0.95),
        first: read(n.first, depth + 1),
        second: read(n.second, depth + 1),
      };
    }
    if (!Array.isArray(n.tabs) || n.tabs.some((key) => typeof key !== "string"))
      throw Error("Invalid tabs");
    return {
      id: n.id,
      tabs: n.tabs,
      active: typeof n.active === "string" ? n.active : undefined,
    };
  }
  try {
    return reconcileLayout(read(JSON.parse(value ?? "null")), keys);
  } catch {
    return { id: "primary", tabs: keys, active: keys[0] };
  }
}

export function dockTab(
  layout: Layout,
  key: string,
  target: string,
  edge?: Edge,
  index?: number,
): Layout {
  const leaves = panes(layout),
    source = leaves.find((p) => p.tabs.includes(key)),
    destination = leaves.find((p) => p.id === target);
  if (
    !source ||
    !destination ||
    (source === destination && (!edge || source.tabs.length === 1))
  )
    return layout;
  let next = updateLayout(layout, source.id, (node) => {
    const pane = node as Pane,
      index = pane.tabs.indexOf(key),
      tabs = pane.tabs.filter((k) => k !== key);
    return {
      ...pane,
      tabs,
      active:
        pane.active === key
          ? tabs[Math.min(index, tabs.length - 1)]
          : pane.active,
    };
  });
  next = updateLayout(next, target, (node) => {
    const pane = node as Pane;
    if (!edge) {
      const tabs = [...pane.tabs];
      tabs.splice(index ?? tabs.length, 0, key);
      return { ...pane, tabs, active: key };
    }
    const added: Pane = { id: crypto.randomUUID(), tabs: [key], active: key };
    return {
      id: crypto.randomUUID(),
      axis: edge === "bottom" ? "y" : "x",
      ratio: edge === "bottom" ? 2 / 3 : 0.5,
      first: edge === "left" ? added : pane,
      second: edge === "left" ? pane : added,
    };
  });
  return reconcileLayout(
    next,
    leaves.flatMap((p) => p.tabs),
    key,
  );
}

export function minimumSize(
  node: Layout,
  gap = gutter,
): { x: number; y: number } {
  if (!isSplit(node)) return minimum;
  const a = minimumSize(node.first, gap),
    b = minimumSize(node.second, gap);
  return node.axis === "x"
    ? { x: a.x + b.x + gap.x, y: Math.max(a.y, b.y) }
    : { x: Math.max(a.x, b.x), y: a.y + b.y + gap.y };
}

export function layoutBoxes(
  layout: Layout,
  width: number,
  height: number,
  gap = gutter,
) {
  const leaves: PaneBox[] = [],
    dividers: (Box & {
      split: Split;
      bounds: Box;
      ratio: number;
      low: number;
      high: number;
    })[] = [];
  function walk(node: Layout, box: Box) {
    if (!isSplit(node)) {
      leaves.push({ ...box, pane: node });
      return;
    }
    const axis = node.axis,
      dimension = axis === "x" ? "width" : "height",
      spacing = gap[axis],
      size = Math.max(1, box[dimension] - spacing);
    const low = minimumSize(node.first, gap)[axis] / size,
      high = 1 - minimumSize(node.second, gap)[axis] / size;
    const ratio = clamp(node.ratio, low, high),
      distance = clamp(Math.round(size * ratio), size * low, size * high);
    const first = { ...box, [dimension]: distance };
    const second = {
      ...box,
      [axis]: box[axis] + distance + spacing,
      [dimension]: size - distance,
    };
    dividers.push({
      ...box,
      [axis]: box[axis] + distance,
      [dimension]: spacing,
      split: node,
      bounds: box,
      ratio,
      low,
      high,
    });
    walk(node.first, first);
    walk(node.second, second);
  }
  const required = minimumSize(layout, gap);
  walk(layout, {
    x: 0,
    y: 0,
    width: Math.max(width, required.x),
    height: Math.max(height, required.y),
  });
  return { leaves, dividers };
}

export function dropRegion(box: Box, edge?: Edge, gap = gutter): Box {
  if (edge === "left") return { ...box, width: (box.width - gap.x) / 2 };
  if (edge === "right")
    return {
      ...box,
      x: box.x + (box.width + gap.x) / 2,
      width: (box.width - gap.x) / 2,
    };
  if (edge === "bottom") {
    const height = Math.max(minimum.y, (box.height - gap.y) / 3);
    return { ...box, y: box.y + box.height - height, height };
  }
  return box;
}
