import {
  parse,
  routePath,
  tabDetails,
  destinations,
  type Destination,
  type Route,
} from "./routes";

export const destinationFor = (route: Route) =>
  tabDetails(routePath(route))?.destination;
export const tabTitle = (tab: WorkspaceTab) =>
  tab.title || tabDetails(tab.path)?.title || "Page not found";
export const tabIcon = (tab: WorkspaceTab) =>
  tabDetails(tab.path)?.icon ?? destinations.configs.icon;

export type WorkspaceTab = {
  key: string;
  id: Destination;
  path: string;
  title?: string;
  opener?: string;
};
export type { Route } from "./routes";

export function visitTab(tabs: WorkspaceTab[], route: Route): WorkspaceTab[] {
  const id = destinationFor(route);
  if (!id) return tabs;
  const path = routePath(route);
  const requested = tabs.find((tab) => tab.key === route.tabKey);
  const existing =
    requested?.id === id
      ? requested
      : !route.tabKey || requested
        ? tabs.find((tab) => tab.path === path)
        : undefined;
  if (existing?.path === path) return tabs;
  return existing
    ? tabs.map((tab) => {
        if (tab.key !== existing.key) return tab;
        const { title, ...rest } = tab;
        return { ...rest, path };
      })
    : [
        ...tabs,
        { key: !requested && route.tabKey ? route.tabKey : path, id, path },
      ];
}

export function restoreTabs(
  value: string | null,
  welcome = false,
): WorkspaceTab[] {
  try {
    const parsed: unknown = JSON.parse(value ?? "null");
    if (Array.isArray(parsed)) {
      const tabs: WorkspaceTab[] = [];
      for (const tab of parsed) {
        if (
          !tab ||
          !Object.hasOwn(destinations, tab.id) ||
          typeof tab.path !== "string" ||
          typeof tab.key !== "string" ||
          !tab.key ||
          tabs.some((t) => t.key === tab.key)
        )
          continue;
        const route = parse("/" + tab.path);
        if (route && destinationFor(route) === tab.id)
          tabs.push({
            key: tab.key,
            id: tab.id,
            path: routePath(route),
            ...(typeof tab.title === "string" ? { title: tab.title } : {}),
            ...(typeof tab.opener === "string" ? { opener: tab.opener } : {}),
          });
      }
      return tabs;
    }
  } catch {}
  return [...(welcome ? ["welcome"] : []), "runs", "configs"].map((id) => ({
    key: id,
    id: id as Destination,
    path: id,
  }));
}

export function openContent(
  tabs: WorkspaceTab[],
  path: string,
  opener: string,
): WorkspaceTab[] {
  const route = parse(path);
  if (!route) return tabs;
  const existing = tabs.find((tab) => tab.path === routePath(route));
  if (existing) return tabs;
  const next = visitTab(tabs, { ...route, tabKey: crypto.randomUUID() });
  return next.map((tab) => (tabs.includes(tab) ? tab : { ...tab, opener }));
}
