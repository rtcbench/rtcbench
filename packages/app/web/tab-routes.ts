import { parse, routePath, type Route } from "./routes";
import {
  PlayIcon,
  FileSlidersIcon,
  VariableIcon,
  SlidersHorizontalIcon,
  BoxesIcon,
  ServerIcon,
  NetworkIcon,
  KeyRoundIcon,
  PlugIcon,
  PlusIcon,
  HouseIcon,
  TerminalIcon,
} from "lucide-react";

export const destinations = {
  runs: { label: "Runs", group: "Runs", icon: PlayIcon },
  configs: { label: "Configs", group: "Configs", icon: FileSlidersIcon },
  "configs/variables": {
    label: "Environments",
    group: "Configs",
    icon: VariableIcon,
  },
  "configs/system-overrides": {
    label: "System overrides",
    group: "Configs",
    icon: SlidersHorizontalIcon,
  },
  "systems/deployments": {
    label: "Deployments",
    group: "Systems",
    icon: BoxesIcon,
  },
  "systems/instances": {
    label: "Instances",
    group: "Systems",
    icon: ServerIcon,
  },
  "systems/networks": {
    label: "Networks",
    group: "Systems",
    icon: NetworkIcon,
  },
  plugins: { label: "Plugins", group: "Plugins", icon: PlugIcon },
  "plugins/new": { label: "New Plugin", group: "Plugins", icon: PlusIcon },
  welcome: { label: "Welcome", group: "RTCBench", icon: HouseIcon },
  terminal: { label: "New Terminal", group: "", icon: TerminalIcon },
} as const;
export type Destination = keyof typeof destinations;
export type WorkspaceTab = {
  key: string;
  id: Destination;
  path: string;
  title?: string;
  opener?: string;
};
export type { Route } from "./routes";

export function tabIcon(tab: WorkspaceTab) {
  if (tab.path.startsWith("systems/keys")) return KeyRoundIcon;
  if (tab.id === "plugins/new" && tab.path !== "plugins/new") return PlugIcon;
  return destinations[tab.id].icon;
}

export function destinationFor({ page, id }: Route): Destination | undefined {
  if (page === "workspace") return undefined;
  if (page === "welcome") return "welcome";
  if (page === "terminal") return "terminal";
  if (page === "plugins") return id ? "plugins/new" : "plugins";
  if (page === "environment") return "configs/variables";
  if (page === "config") return "configs";
  if (page === "configs")
    return id?.startsWith("variables")
      ? "configs/variables"
      : id === "system-overrides"
        ? "configs/system-overrides"
        : "configs";
  if (page === "systems") {
    const section = id?.split("/")[0];
    return section === "instances" || section === "keys"
      ? "systems/instances"
      : section === "networks"
        ? "systems/networks"
        : "systems/deployments";
  }
  return "runs";
}

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

export function tabTitle(tab: WorkspaceTab) {
  if (tab.title) return tab.title;
  const { page, id } = parse(tab.path)!;
  if (page === "terminal") return "Terminal";
  if (page === "config" || page === "environment") return id!;
  if (page === "run") return "Run";
  if (page === "plugins" && id && id !== "new") return id;
  if (page === "configs" && id?.endsWith("new"))
    return id === "new" ? "New config" : "New environment";
  if (page === "systems" && id?.includes("/")) {
    const section = id.split("/").at(-2)!;
    const name = (
      {
        instances: "instance",
        deployments: "deployment",
        networks: "network",
        keys: "SSH key",
      } as Record<string, string>
    )[section];
    return `${id.endsWith("/new") ? "New " : ""}${name ?? "System"}`;
  }
  if (page === "systems" && id === "keys") return "SSH keys";
  return destinations[tab.id].label;
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
