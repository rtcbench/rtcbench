import { useWorkspaceIds, useTabSize, fitTabWidth } from "./context";
import {
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
  type CSSProperties,
} from "react";
import { ActionMenu } from "./action-menu";
import {
  dockTab,
  dropRegion,
  layoutBoxes,
  minimum,
  minimumSize,
  panes,
  updateLayout,
  type Box,
  type Edge,
  type Layout,
  type Pane,
} from "./layout";
import type { TabDrag } from "./tabs";

export const position = (box: Box): CSSProperties => ({
  left: box.x,
  top: box.y,
  width: box.width,
  height: box.height,
});
export type Docking = {
  move: (drag: TabDrag) => boolean;
  drop: () => boolean;
  cancel: () => void;
  split: (key: string, edge: Edge) => boolean;
  next: (key: string) => void;
  paneCount: number;
  insertion?: number;
};
type Preview = { key: string; pane?: string; edge?: Edge; index?: number };

export function WorkspaceSurface({
  layout,
  change,
  header,
  content,
}: {
  layout: Layout;
  change: (layout: Layout) => void;
  header: (
    pane: Pane,
    first: boolean,
    docking: Docking,
    surface: (key?: string) => HTMLElement | null,
  ) => ReactNode;
  content: (
    panes: { pane: Pane; box: Box }[],
    transition: boolean,
    lifted?: string,
  ) => ReactNode;
}) {
  const ids = useWorkspaceIds();
  const {
    height: tabHeight,
    width: tabWidth,
    columnGap,
    rowGap,
  } = useTabSize();
  const gutter = { x: columnGap, y: rowGap };
  const board = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState({ width: 0, height: 0 });
  const [preview, setPreview] = useState<Preview>();
  const previewRef = useRef<Preview | undefined>(undefined);
  const [resizing, setResizing] = useState(false);
  const geometry = [
    size.width,
    size.height,
    tabHeight,
    columnGap,
    rowGap,
  ].join();
  const previousGeometry = useRef(geometry);
  const stableGeometry = previousGeometry.current === geometry;
  useLayoutEffect(() => {
    previousGeometry.current = geometry;
  }, [geometry]);
  useLayoutEffect(() => {
    const observer = new ResizeObserver(([entry]) => {
      const { width, height } = entry!.contentRect;
      setSize({ width, height });
    });
    observer.observe(board.current!);
    return () => observer.disconnect();
  }, []);
  const { leaves, dividers } = layoutBoxes(
      layout,
      size.width,
      size.height,
      gutter,
    ),
    required = minimumSize(layout, gutter);
  const show = (next?: Preview) => {
    if (
      previewRef.current?.pane === next?.pane &&
      previewRef.current?.edge === next?.edge &&
      previewRef.current?.index === next?.index &&
      previewRef.current?.key === next?.key
    )
      return;
    previewRef.current = next;
    setPreview(next);
  };
  const canSplit = (box: Box, edge: Edge) =>
    edge === "bottom"
      ? box.height >= minimum.y * 2 + rowGap
      : box.width >= minimum.x * 2 + columnGap;
  const docking: Docking = {
    paneCount: leaves.length,
    next: (key) => {
      const index = leaves.findIndex((p) => p.pane.tabs.includes(key));
      change(
        dockTab(layout, key, leaves[(index + 1) % leaves.length]!.pane.id),
      );
    },
    move: ({ key, x, y, detached: wasDetached }) => {
      const bounds = board.current!.getBoundingClientRect(),
        localX = x - bounds.left,
        localY = y - bounds.top;
      const target = leaves.find(
        (p) =>
          localX >= p.x &&
          localX <= p.x + p.width &&
          localY >= p.y &&
          localY <= p.y + p.height,
      );
      const source = leaves.find((p) => p.pane.tabs.includes(key))!;
      const margin = wasDetached ? 8 : 24;
      const detached =
        localY > source.y + tabHeight + margin ||
        localY < source.y - margin ||
        (!!target &&
          (localX < source.x - margin ||
            localX > source.x + source.width + margin));
      if (!detached) {
        show();
        return false;
      }
      let edge: Edge | undefined;
      if (target && localY > target.y + tabHeight) {
        if (localY > target.y + target.height * 0.72) edge = "bottom";
        else if (localX < target.x + target.width * 0.28) edge = "left";
        else if (localX > target.x + target.width * 0.72) edge = "right";
      }
      const valid =
        target &&
        (!edge || canSplit(target, edge)) &&
        (!target.pane.tabs.includes(key) ||
          (!!edge && target.pane.tabs.length > 1));
      let index: number | undefined;
      if (valid && !edge) {
        const nav = board.current!.querySelector<HTMLElement>(
          `[data-pane-header="${CSS.escape(target.pane.id)}"] [role="tablist"]`,
        );
        const header = nav?.closest<HTMLElement>("[data-pane-header]");
        index = target.pane.tabs.length;
        if (nav && header && localY <= target.y + tabHeight) {
          const strip = nav.parentElement!;
          const inset =
            strip.getBoundingClientRect().left -
            header.getBoundingClientRect().left;
          const trailing =
            strip.parentElement!.nextElementSibling?.getBoundingClientRect()
              .width ?? 0;
          const available = target.width - inset - trailing - 4;
          const width = fitTabWidth(
            tabWidth,
            available,
            target.pane.tabs.length + 1,
          );
          const overflow =
            width * (target.pane.tabs.length + 1) + 16 - available;
          const arrows = overflow > 1 ? Math.min(28, tabHeight) : 0;
          const scroll = Math.min(
            nav.scrollLeft,
            Math.max(0, overflow + arrows * 2),
          );
          index = Math.max(
            0,
            Math.min(
              target.pane.tabs.length,
              Math.round(
                (localX - target.x - inset - arrows + scroll - 8) / width,
              ),
            ),
          );
        }
      }
      show(valid ? { pane: target.pane.id, key, edge, index } : { key });
      return true;
    },
    drop: () => {
      const target = previewRef.current;
      show();
      if (!target?.pane) return false;
      change(
        dockTab(layout, target.key, target.pane, target.edge, target.index),
      );
      return true;
    },
    cancel: () => show(),
    split: (key, edge) => {
      const target = leaves.find((p) => p.pane.tabs.includes(key));
      if (!target || target.pane.tabs.length < 2 || !canSplit(target, edge))
        return false;
      change(dockTab(layout, key, target.pane.id, edge));
      return true;
    },
  };
  const lifted = preview?.key;
  const regions = leaves.map(({ pane, ...box }) => {
    if (lifted && pane.tabs.includes(lifted)) {
      const remaining = pane.tabs.filter((key) => key !== lifted);
      pane = {
        ...pane,
        active:
          remaining[Math.min(pane.tabs.indexOf(lifted), remaining.length - 1)],
      };
    }
    if (preview?.pane !== pane.id || !preview.edge) return { pane, box };
    const drop = dropRegion(box, preview.edge, gutter);
    if (preview.edge === "left")
      return {
        pane,
        box: { ...box, x: drop.x + drop.width + columnGap, width: drop.width },
      };
    if (preview.edge === "right")
      return { pane, box: { ...box, width: drop.width } };
    return { pane, box: { ...box, height: drop.y - box.y - rowGap } };
  });
  const transition = !resizing && size.width > 0 && stableGeometry;
  return (
    <div className="lab:min-h-0 lab:flex-1 lab:overflow-auto">
      <div
        ref={board}
        className="lab:relative lab:isolate lab:h-full lab:w-full lab:overflow-clip"
        style={{
          minWidth: required.x,
          minHeight: required.y,
        }}
      >
        {regions.map(({ pane, box }, index) => (
          <div
            key={pane.id}
            data-pane-header={pane.id}
            className="lab:transition-[left,top,width,height] lab:duration-180 lab:ease-out lab:motion-reduce:transition-none lab:absolute lab:z-30"
            style={{
              ...position({ ...box, height: tabHeight }),
              transitionDuration: transition
                ? "var(--workspace-duration, 180ms)"
                : "0ms",
            }}
          >
            {header(
              pane,
              index === 0,
              {
                ...docking,
                insertion:
                  preview?.pane === pane.id ? preview.index : undefined,
              },
              (key = pane.active) =>
                board.current?.querySelector<HTMLElement>(
                  key
                    ? `[id="${CSS.escape(ids.panel(key))}"]`
                    : `[data-pane-sheet="${CSS.escape(pane.id)}"]:not([hidden]):not([data-drag-content])`,
                ) ?? null,
            )}
          </div>
        ))}
        {content(regions, transition, lifted)}
        {dividers.map(({ split, bounds, ratio, low, high, ...box }) => {
          const axis = split.axis,
            horizontal = axis === "x",
            dimension = horizontal ? "width" : "height";
          const primary = panes(split.first).filter((p) => p.active);
          const resize = (value: number) =>
            change(
              updateLayout(layout, split.id, (node) => ({
                ...node,
                ratio: Math.max(low, Math.min(high, value)),
              })),
            );
          return (
            <ActionMenu
              key={split.id}
              actions={[
                [
                  horizontal ? "Narrower" : "Shorter",
                  () => resize(ratio - 0.1),
                ],
                [horizontal ? "Wider" : "Taller", () => resize(ratio + 0.1)],
                ["Reset size", () => resize(horizontal ? 0.5 : 2 / 3)],
              ]}
            >
              <div
                role="separator"
                aria-label={horizontal ? "Resize columns" : "Resize rows"}
                aria-labelledby={primary
                  .map((p) => ids.tab(p.active!))
                  .join(" ")}
                aria-controls={primary
                  .map((p) => ids.panel(p.active!))
                  .join(" ")}
                aria-valuetext={`${Math.round(ratio * 100)}% ${horizontal ? "width" : "height"}`}
                title="Drag to resize, use arrow keys, or open resize actions with right-click or Shift+F10"
                aria-orientation={horizontal ? "vertical" : "horizontal"}
                aria-valuenow={Math.round(ratio * 100)}
                aria-valuemin={Math.round(low * 100)}
                aria-valuemax={Math.round(high * 100)}
                tabIndex={0}
                className={
                  "lab:absolute lab:z-20 lab:touch-none lab:select-none lab:rounded lab:outline-none lab:focus-visible:ring-2 lab:focus-visible:ring-ring " +
                  (horizontal
                    ? "lab:cursor-col-resize"
                    : "lab:cursor-row-resize")
                }
                style={position({
                  ...box,
                  [axis]: box[axis] + gutter[axis] / 2 - 12,
                  [dimension]: 24,
                })}
                onDoubleClick={() => resize(horizontal ? 0.5 : 2 / 3)}
                onPointerDown={(event) => {
                  if (event.button !== 0) return;
                  event.preventDefault();
                  event.currentTarget.setPointerCapture(event.pointerId);
                  setResizing(true);
                }}
                onPointerMove={(event) => {
                  if (!event.currentTarget.hasPointerCapture(event.pointerId))
                    return;
                  const b = board.current!.getBoundingClientRect();
                  const coordinate = horizontal
                    ? event.clientX - b.left
                    : event.clientY - b.top;
                  resize(
                    (coordinate - bounds[axis]) /
                      (bounds[dimension] - gutter[axis]),
                  );
                }}
                onLostPointerCapture={() => setResizing(false)}
                onKeyDown={(event) => {
                  const step = event.shiftKey ? 0.1 : 0.02;
                  const values: Record<string, number> = {
                    [horizontal ? "ArrowLeft" : "ArrowUp"]: ratio - step,
                    [horizontal ? "ArrowRight" : "ArrowDown"]: ratio + step,
                    Home: low,
                    End: high,
                  };
                  const value = values[event.key];
                  if (value === undefined) return;
                  event.preventDefault();
                  resize(value);
                }}
              />
            </ActionMenu>
          );
        })}
        <span role="status" className="lab:sr-only">
          {preview?.pane &&
            (preview.edge ? `Split ${preview.edge}` : "Move tab here")}
        </span>
      </div>
    </div>
  );
}
