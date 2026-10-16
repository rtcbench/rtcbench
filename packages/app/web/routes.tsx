import {
  matchRoutes,
  matchPath,
  parsePath,
  renderMatches,
  useParams,
  type Params,
  type NonIndexRouteObject,
} from "react-router";
import { Page, FieldError } from "@labpane/ui";
import { Link } from "./integration";
import { ConfigPage, ConfigsPage } from "./configs";
import { EnvPage, SystemOverrides, VariableSets } from "./envs";
import { RunPage } from "./run";
import { RunsPage } from "./runs";
import { BenchmarkPage, BenchmarksPage } from "./benchmarks";
import { SystemsPage } from "./systems/page";
import { systemRoute, systemSections } from "./systems/routes";
import { PluginsPage, NewPluginPage } from "./plugins";
import { TerminalPage } from "./terminal-page";
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
  benchmarks: { label: "Benchmarks", group: "Benchmarks", icon: PlayIcon },
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
export type Destination = keyof typeof destinations | "not-found";

export type Route = { page: string; id?: string; tabKey?: string };
export const routePath = ({ page, id }: Route) =>
  page === "not-found"
    ? (id ?? "not-found")
    : page + (id ? "/" + id.split("/").map(encodeURIComponent).join("/") : "");

type Value = string | ((params: Params) => string | undefined);
type Handle = {
  page: string;
  id?: Value;
  destination?: Destination | ((params: Params) => Destination);
  title?: Value;
  icon?: typeof PlayIcon;
  valid?: (params: Params) => boolean;
};
type PageRoute = Omit<NonIndexRouteObject, "handle" | "children"> & {
  handle: Handle;
};
const value = (field: Value | undefined, params: Params) =>
  typeof field === "function" ? field(params) : field;
const resource = ({ id }: Params) => id;
const documentName = (params: Params) => params["*"];
const systemID = (params: Params) => params["*"] || "deployments";
const terminalID = ({ kind, id }: Params) => `${kind}/${id}`;

function ConfigRoute() {
  const name = useParams()["*"];
  return <ConfigPage key={name} name={name} />;
}
function EnvironmentRoute() {
  const name = useParams()["*"];
  return <EnvPage key={name} name={name} />;
}
function RunRoute() {
  const { id } = useParams();
  return <RunPage key={id} id={id!} />;
}
function BenchmarkRoute() {
  const { id } = useParams();
  return <BenchmarkPage key={id} id={id!} />;
}
function PluginRoute() {
  const { id } = useParams();
  return <NewPluginPage key={id} name={id} />;
}
function TerminalRoute() {
  return <TerminalPage target={terminalID(useParams())} />;
}
function SystemRoute() {
  return <SystemsPage path={systemID(useParams())} />;
}
function NotFound() {
  return (
    <Page title="Page not found">
      <FieldError>This page does not exist.</FieldError>
      <Link href="runs">Runs</Link>
    </Page>
  );
}

const systemDestination = (params: Params): Destination => {
  const section = systemRoute(systemID(params))?.section;
  return section === "keys"
    ? "systems/instances"
    : `systems/${section ?? "deployments"}`;
};
const systemTitle = (params: Params) => {
  const route = systemRoute(systemID(params));
  if (!route) return undefined;
  const step = route.steps.at(-1);
  if (!step) return systemSections[route.section];
  const name = {
    deployments: "deployment",
    instances: "instance",
    networks: "network",
    keys: "SSH key",
  }[step.section];
  return `${step.id === "new" ? "New " : ""}${name}`;
};

const routes: PageRoute[] = [
  {
    path: "welcome",
    element: null,
    handle: { page: "welcome", destination: "welcome" },
  },
  { path: "workspace", element: null, handle: { page: "workspace" } },
  {
    path: "runs",
    Component: RunsPage,
    handle: { page: "runs", destination: "runs" },
  },
  {
    path: "run/:id",
    Component: RunRoute,
    handle: { page: "run", id: resource, destination: "runs", title: "Run" },
  },
  {
    path: "benchmarks",
    Component: BenchmarksPage,
    handle: { page: "benchmarks", destination: "benchmarks" },
  },
  {
    path: "benchmarks/new",
    element: <BenchmarkPage key="new" id="new" />,
    handle: {
      page: "benchmarks",
      id: "new",
      destination: "benchmarks",
      title: "New benchmark",
    },
  },
  {
    path: "benchmarks/:id",
    Component: BenchmarkRoute,
    handle: {
      page: "benchmarks",
      id: resource,
      destination: "benchmarks",
      title: "Benchmark",
    },
  },
  {
    path: "configs",
    Component: ConfigsPage,
    handle: { page: "configs", destination: "configs" },
  },
  {
    path: "configs/new",
    element: <ConfigPage key="new" />,
    handle: {
      page: "configs",
      id: "new",
      destination: "configs",
      title: "New config",
    },
  },
  {
    path: "config/*",
    Component: ConfigRoute,
    handle: {
      page: "config",
      id: documentName,
      destination: "configs",
      title: documentName,
      valid: (params) => !!params["*"],
    },
  },
  {
    path: "configs/variables",
    Component: VariableSets,
    handle: {
      page: "configs",
      id: "variables",
      destination: "configs/variables",
    },
  },
  {
    path: "configs/variables/new",
    element: <EnvPage key="new" />,
    handle: {
      page: "configs",
      id: "variables/new",
      destination: "configs/variables",
      title: "New environment",
    },
  },
  {
    path: "environment/*",
    Component: EnvironmentRoute,
    handle: {
      page: "environment",
      id: documentName,
      destination: "configs/variables",
      title: documentName,
      valid: (params) => !!params["*"],
    },
  },
  {
    path: "configs/system-overrides",
    Component: SystemOverrides,
    handle: {
      page: "configs",
      id: "system-overrides",
      destination: "configs/system-overrides",
    },
  },
  {
    path: "systems/*",
    Component: SystemRoute,
    handle: {
      page: "systems",
      id: systemID,
      destination: systemDestination,
      title: systemTitle,
      valid: (params) => !!systemRoute(systemID(params)),
    },
  },
  {
    path: "plugins",
    Component: PluginsPage,
    handle: { page: "plugins", destination: "plugins" },
  },
  {
    path: "plugins/new",
    element: <NewPluginPage key="new" />,
    handle: { page: "plugins", id: "new", destination: "plugins/new" },
  },
  {
    path: "plugins/:id",
    Component: PluginRoute,
    handle: {
      page: "plugins",
      id: resource,
      destination: "plugins/new",
      title: resource,
      icon: PlugIcon,
      valid: ({ id }) => /^[a-z][a-z0-9-]{0,47}$/.test(id!),
    },
  },
  {
    path: "terminal",
    Component: TerminalPage,
    handle: { page: "terminal", destination: "terminal", title: "Terminal" },
  },
  {
    path: "terminal/:kind/:id",
    Component: TerminalRoute,
    handle: {
      page: "terminal",
      id: terminalID,
      destination: "terminal",
      title: "Terminal",
      valid: ({ kind, id }) =>
        ["plugins", "instances", "deployments"].includes(kind!) &&
        /^[a-zA-Z0-9-]+$/.test(id!),
    },
  },
  {
    path: "*",
    Component: NotFound,
    handle: {
      page: "not-found",
      id: documentName,
      destination: "not-found",
      title: "Page not found",
      icon: FileSlidersIcon,
    },
  },
];

function match(path: string) {
  const pathname = parsePath("/" + path.replace(/^\//, "")).pathname!;
  try {
    if (decodeURIComponent(pathname).endsWith(".yml")) return null;
  } catch {
    return null;
  }
  const matches = matchRoutes<PageRoute>(routes, pathname);
  const found = matches?.at(-1);
  const raw = found?.route.path && matchPath(found.route.path, pathname);
  if (raw && found)
    found.params = Object.fromEntries(
      Object.entries(raw.params).map(([key, value]) => [
        key,
        value === undefined ? value : decodeURIComponent(value),
      ]),
    );
  if (
    !found ||
    (found.route.handle.valid && !found.route.handle.valid(found.params))
  )
    return null;
  return matches;
}

export function parse(path: string): Route | null {
  const found = match(path)?.at(-1);
  if (!found || found.route.handle.page === "not-found") return null;
  const { page, id } = found.route.handle;
  return { page, id: value(id, found.params) };
}

export function tabDetails(path: string) {
  const found = match(path)?.at(-1);
  if (!found)
    return {
      destination: "not-found" as const,
      title: "Page not found",
      icon: FileSlidersIcon,
    };
  const { handle } = found.route;
  const destination =
    typeof handle.destination === "function"
      ? handle.destination(found.params)
      : handle.destination;
  if (!destination) return undefined;
  const menu =
    destination === "not-found" ? undefined : destinations[destination];
  return {
    destination,
    title: value(handle.title, found.params) ?? menu?.label ?? "Page not found",
    icon:
      handle.icon ??
      (handle.page === "systems" &&
      systemRoute(systemID(found.params))?.section === "keys"
        ? KeyRoundIcon
        : menu?.icon) ??
      FileSlidersIcon,
  };
}

export function RoutePage({ path }: { path: string }) {
  const matches = match(path);
  return matches ? renderMatches(matches) : <NotFound />;
}
