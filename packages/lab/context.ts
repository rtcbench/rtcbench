import { createContext, useContext, useMemo } from "react";

export const WorkspaceId = createContext("");
export const densityPresets = {
  comfortable: {
    value: 100,
    width: 192,
    height: 44,
    contentHeight: 36,
    columnGap: 8,
    rowGap: 8,
    logoSize: 24,
    borderWidth: 1,
  },
  compact: {
    value: 50,
    width: 168,
    height: 34,
    contentHeight: 30,
    columnGap: 6,
    rowGap: 4,
    logoSize: 20,
    borderWidth: 0.75,
  },
  dense: {
    value: 0,
    width: 152,
    height: 26,
    contentHeight: 24,
    columnGap: 4,
    rowGap: 2,
    logoSize: 16,
    borderWidth: 0.5,
  },
};
export type Density = number;
export function densitySize(density: Density) {
  const value = Number.isFinite(density)
    ? Math.max(0, Math.min(100, density))
    : 100;
  const { dense, compact, comfortable } = densityPresets;
  const [from, to] =
    value < compact.value ? [dense, compact] : [compact, comfortable];
  const amount = (value - from.value) / (to.value - from.value);
  return Object.fromEntries(
    (Object.keys(from) as (keyof typeof from)[]).map((key) => [
      key,
      from[key] + (to[key] - from[key]) * amount,
    ]),
  ) as typeof from;
}
export const WorkspaceDensity = createContext<Density>(100);
export const fitTabWidth = (width: number, available: number, count: number) =>
  Math.min(width, Math.max(128, (available - 16) / Math.max(1, count)));
export function useTabSize() {
  const density = useContext(WorkspaceDensity);
  return useMemo(() => densitySize(density), [density]);
}
export function useWorkspaceIds() {
  const prefix = useContext(WorkspaceId);
  return {
    tab: (key: string) => `${prefix}-tab-${encodeURIComponent(key)}`,
    panel: (key: string) => `${prefix}-panel-${encodeURIComponent(key)}`,
  };
}
