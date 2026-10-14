import { useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { PlusIcon, RotateCwIcon } from "lucide-react";
import {
  ActionMenu,
  Button,
  DiscardDialog,
  OptionMenu,
  type MenuOption,
} from "@labpane/ui";
import {
  Workspace,
  WorkspaceFrame,
  restoreLayout,
  reconcileLayout,
  dockTab,
  panes,
  densitySize,
  type WorkspaceDensity,
  useUnsavedChanges,
  desktopSlots,
  type WindowHost,
} from "@labpane/os";

import { parse, routePath, type Route } from "./routes";
import { PageScope, PageTab, type PageOptions } from "./integration";
import { ConfigPage, ConfigsPage } from "./configs";
import { EnvPage, SystemOverrides, VariableSets } from "./envs";
import { RunPage } from "./run";
import { RunsPage } from "./runs";
import { SystemsPage } from "./systems/page";
import { AppMenu } from "./app-menu";
import {
  setTerminalTheme,
  terminalThemeNames,
  useTerminalTheme,
} from "./terminal-theme";
import { PluginsPage, NewPluginPage } from "./plugins";
import { TerminalPage } from "./terminal-page";
import { NewTerminal } from "./new-terminal";
import {
  destinationFor,
  destinations,
  restoreTabs,
  visitTab,
  openContent,
  tabTitle,
  tabIcon,
  type WorkspaceTab,
} from "./tab-routes";

const options: MenuOption[] = Object.entries(destinations).map(
  ([id, item]) => ({
    id,
    label: item.label,
    group: id === "runs" ? undefined : item.group,
    icon: (
      <item.icon aria-hidden="true" className="labui:size-4 labui:shrink-0" />
    ),
  }),
);
function readSaved(key: string | undefined) {
  try {
    return key ? sessionStorage.getItem(key) : null;
  } catch {
    return null;
  }
}
function save(key: string | undefined, value: unknown) {
  try {
    if (key) sessionStorage.setItem(key, JSON.stringify(value));
  } catch {}
}
export type RTCBenchProps = Pick<PageOptions, "apiUrl" | "fetch" | "href"> & {
  welcome?: boolean;
  route?: Route;
  onRouteChange?: (route: Route, replace: boolean) => void;
  storageKey?: string;
  className?: string;
  dark?: boolean;
  onThemeChange?: (dark: boolean) => void;
  density?: WorkspaceDensity;
  onDensityChange?: (density: WorkspaceDensity) => void;
  onDensityReset?: () => void;
  // host is the desktop window the app fills; the window is the frame, and Labpane puts the menu
  // and the window's controls where the platform expects them
  host?: WindowHost;
  fullscreen?: boolean;
  onFullscreenChange?: (fullscreen: boolean) => void;
};

export function RTCBench({
  welcome = false,
  route: controlledRoute,
  onRouteChange,
  storageKey,
  className = "",
  dark = false,
  onThemeChange,
  density = 40,
  onDensityChange,
  onDensityReset,
  host,
  fullscreen,
  onFullscreenChange,
  ...settings
}: RTCBenchProps) {
  const [localRoute, setRoute] = useState<Route>({
    page: welcome ? "welcome" : "runs",
  });
  const route = controlledRoute ?? localRoute;
  const storeKey = (suffix: string) =>
    storageKey ? storageKey + suffix : undefined;
  const changeRoute = (next: Route, replace = false) => {
    setRoute(next);
    onRouteChange?.(next, replace);
  };
  const go = (path: string, tabKey?: string) =>
    changeRoute({ ...(parse(path) ?? { page: "runs" }), tabKey });
  const [expanded, setExpanded] = useState(
    () => readSaved(storeKey(":expanded")) === "true",
  );
  const [terminalOpener, setTerminalOpener] = useState<string>();
  const [menuPane, setMenuPane] = useState<string>();
  const openedTab = useRef<string | undefined>(undefined);
  const [discard, setDiscard] = useState<{
    label: string;
    resolve: (discard: boolean) => void;
  }>();
  const control = densitySize(density).contentHeight;
  const terminalTheme = useTerminalTheme();
  const [terminalOpened, setTerminalOpened] = useState(false);
  const refresh = useRef(new Map<string, () => void>());
  const [tabs, setTabs] = useState(() =>
    visitTab(restoreTabs(readSaved(storageKey), welcome), route),
  );
  const [layout, setLayout] = useState(() =>
    restoreLayout(
      readSaved(storeKey(":layout")),
      tabs.map((t) => t.key),
    ),
  );
  const visibleTabs = visitTab(tabs, route);
  const active =
    visibleTabs.find(
      (tab) => tab.key === route.tabKey && tab.id === destinationFor(route),
    ) ?? visibleTabs.find((tab) => tab.path === routePath(route));
  useLayoutEffect(() => {
    setTabs(visibleTabs);
    save(storageKey, visibleTabs);
    if (route.tabKey !== active?.key)
      changeRoute({ ...route, tabKey: active?.key }, true);
  }, [visibleTabs, active?.key]);
  useLayoutEffect(() => save(storeKey(":layout"), layout), [layout]);
  useLayoutEffect(() => save(storeKey(":expanded"), expanded), [expanded]);
  function open(path: string, opener: string) {
    const terminal = parse(path)?.page === "terminal";
    const next = terminal
      ? visitTab(visibleTabs, { ...parse(path)!, tabKey: crypto.randomUUID() })
      : openContent(visibleTabs, path, opener);
    const target = terminal
      ? next.at(-1)
      : next.find((t) => t.path === routePath(parse(path)!));
    if (!target) return "";
    if (terminal) {
      target.opener = opener;
      const leaves = panes(layout),
        pane = leaves.find((p) => p.tabs.includes(opener));
      if (pane)
        setLayout(
          dockTab(
            reconcileLayout(
              layout,
              next.map((t) => t.key),
              opener,
              pane.id,
            ),
            target.key,
            pane.id,
            !terminalOpened && leaves.length === 1 ? "bottom" : undefined,
          ),
        );
      setTerminalOpened(true);
    }
    setTabs(next);
    go(target.path, target.key);
    return target.key;
  }
  const menu = (align: "start" | "end") => (
    <PageScope {...settings}>
      <AppMenu
        dark={dark}
        onThemeChange={onThemeChange}
        density={density}
        onDensityChange={onDensityChange}
        onDensityReset={onDensityReset}
        fullscreen={fullscreen}
        onFullscreenChange={onFullscreenChange}
        align={align}
      />
    </PageScope>
  );
  const frame = (workspace: ReactNode) =>
    host ? (
      workspace
    ) : (
      <WorkspaceFrame expanded={expanded} change={setExpanded}>
        {workspace}
      </WorkspaceFrame>
    );
  return (
    <div
      className={`${dark ? "dark" : ""} labui:h-full labui:min-h-0 labui:bg-background labui:text-foreground labui:overflow-hidden labui:p-1.5 ${host ? "" : "labui:sm:px-2 labui:lg:px-3"} ${className}`}
    >
      {frame(
        <Workspace
          density={density}
          titleBar={!!host}
          tabs={visibleTabs.map((tab) => {
            const Icon = tabIcon(tab);
            return {
              ...tab,
              label: tabTitle(tab),
              icon: <Icon aria-hidden="true" className="labui:size-4" />,
              actions: tab.id === "terminal" && (
                <Button
                  variant="ghost"
                  size="icon-xs"
                  aria-label={`Reconnect ${tabTitle(tab)}`}
                  title="Reconnect terminal"
                  onClick={() => refresh.current.get(tab.key)?.()}
                >
                  <RotateCwIcon aria-hidden="true" className="labui:size-4" />
                </Button>
              ),
              href: settings.href?.(tab.path) ?? "/" + tab.path,
              scrollKey: tab.path,
              fill: tab.id === "terminal",
            };
          })}
          active={active?.key}
          layout={layout}
          onLayoutChange={setLayout}
          {...desktopSlots(host, menu)}
          toolbar={(pane) => (
            <OptionMenu
              open={menuPane === pane.id}
              onOpenChange={(open) => setMenuPane(open ? pane.id : undefined)}
              options={options}
              onSelect={(id) => {
                if (id === "terminal") {
                  setTerminalOpener(pane.active ?? active?.key ?? "");
                  return;
                }
                const key = crypto.randomUUID();
                go(id, key);
                openedTab.current = key;
              }}
              onCloseAutoFocus={(event) => {
                const key = openedTab.current;
                if (!key) return;
                event.preventDefault();
                openedTab.current = undefined;
                pane.focus(key);
              }}
              trigger={
                <button
                  type="button"
                  className="labui:inline-flex labui:shrink-0 labui:items-center labui:justify-center labui:rounded-full labui:text-sm labui:font-medium labui:outline-none labui:transition-colors labui:hover:bg-accent labui:focus-visible:ring-2 labui:focus-visible:ring-ring"
                  style={{ width: control, height: control }}
                  aria-label="Open a tab"
                  title="Open a tab"
                >
                  <PlusIcon aria-hidden="true" className="labui:size-4" />
                </button>
              }
            />
          )}
          menu={(target, actions, restoreFocus, key) => (
            <ActionMenu
              actions={
                visibleTabs.find((tab) => tab.key === key)?.id === "terminal"
                  ? [
                      ...actions,
                      null,
                      {
                        label: "Theme",
                        items: terminalThemeNames.map((name) => ({
                          label: name[0]!.toUpperCase() + name.slice(1),
                          checked: name === terminalTheme,
                          onSelect: () => setTerminalTheme(name),
                        })),
                      },
                    ]
                  : actions
              }
              onCloseAutoFocus={
                restoreFocus &&
                ((event) => {
                  event.preventDefault();
                  restoreFocus();
                })
              }
            >
              {target}
            </ActionMenu>
          )}
          confirmClose={(tab) =>
            new Promise<boolean>((resolve) =>
              setDiscard({ label: tab.label, resolve }),
            )
          }
          onSelect={(key) => {
            const tab = visibleTabs.find((t) => t.key === key)!;
            if (active?.key !== key) go(tab.path, key);
          }}
          onDuplicate={(key) => {
            const copy = {
              ...visibleTabs.find((tab) => tab.key === key)!,
              key: crypto.randomUUID(),
              opener: key,
            };
            setTabs([...visibleTabs, copy]);
            go(copy.path, copy.key);
            return copy.key;
          }}
          onClose={(key, neighbor) => {
            const next: WorkspaceTab[] = visibleTabs.filter(
              (tab) => tab.key !== key,
            );
            if (active?.key === key)
              go(
                next.find((tab) => tab.key === neighbor)?.path ?? "workspace",
                neighbor,
              );
            setTabs(next);
            save(storageKey, next);
          }}
          empty={(pane) => <Welcome empty open={() => setMenuPane(pane.id)} />}
        >
          {(tab, pane) =>
            tab.id === "welcome" ? (
              <Welcome open={() => setMenuPane(pane.id)} />
            ) : (
              <TabPage
                {...settings}
                route={parse(tab.path)!}
                navigate={(path) => go(path, tab.key)}
                tab={{
                  selected: active?.key === tab.key,
                  refresh: (handler) => {
                    if (handler) refresh.current.set(tab.key, handler);
                    else refresh.current.delete(tab.key);
                  },
                  open: (path) => {
                    open(path, tab.key);
                  },
                  title: (title) =>
                    setTabs((previous) =>
                      previous.find((t) => t.key === tab.key)?.title === title
                        ? previous
                        : previous.map((t) =>
                            t.key === tab.key ? { ...t, title } : t,
                          ),
                    ),
                }}
              />
            )
          }
        </Workspace>,
      )}
      <DiscardDialog
        label={discard?.label}
        onCancel={() => {
          discard?.resolve(false);
          setDiscard(undefined);
        }}
        onConfirm={() => {
          discard?.resolve(true);
          setDiscard(undefined);
        }}
        onCloseAutoFocus={(event) => event.preventDefault()}
      />
      {terminalOpener !== undefined && (
        <PageScope {...settings}>
          <NewTerminal
            close={() => setTerminalOpener(undefined)}
            open={(path) => {
              open(path, terminalOpener);
            }}
          />
        </PageScope>
      )}
    </div>
  );
}

function Welcome({
  empty = false,
  open,
}: {
  empty?: boolean;
  open: () => void;
}) {
  return (
    <div className="labui:flex labui:min-h-full labui:flex-col labui:items-center labui:justify-center labui:gap-4 labui:p-3">
      {!empty && (
        <h1 className="labui:font-display labui:text-3xl labui:font-bold labui:tracking-tight">
          Welcome
        </h1>
      )}
      <button
        type="button"
        onClick={open}
        className="labui:flex labui:flex-col labui:items-center labui:gap-4 labui:rounded-lg labui:p-8 labui:text-muted-foreground labui:outline-none labui:hover:text-foreground labui:focus-visible:outline-2 labui:focus-visible:outline-ring"
      >
        <PlusIcon aria-hidden="true" className="labui:size-12 labui:stroke-1" />
        <span>Open a tab</span>
      </button>
    </div>
  );
}

function TabPage({
  route,
  tab,
  ...settings
}: PageOptions & {
  route: Route;
  tab: NonNullable<React.ContextType<typeof PageTab>>;
}) {
  const [dirty, setDirty] = useState(false);
  useUnsavedChanges(dirty);
  return (
    <PageTab.Provider value={tab}>
      <PageScope {...settings} onDirtyChange={setDirty}>
        <PageContent {...route} />
      </PageScope>
    </PageTab.Provider>
  );
}

function PageContent({ page, id }: Route) {
  if (page === "welcome") return null;
  if (page === "terminal") return <TerminalPage target={id} />;
  if (page === "plugins")
    return id ? (
      <NewPluginPage key={id} name={id === "new" ? undefined : id} />
    ) : (
      <PluginsPage />
    );
  if (page === "environment") return <EnvPage key={id} name={id} />;
  if (page === "systems") return <SystemsPage path={id} />;
  if (page === "run" && id) return <RunPage key={id} id={id} />;
  if (page === "config" && id) return <ConfigPage key={id} name={id} />;
  if (page === "configs" && id === "new") return <ConfigPage key="new" />;
  if (page === "configs" && id === "variables/new")
    return <EnvPage key="new-variables" />;
  if (page === "configs" && id === "variables") return <VariableSets />;
  if (page === "configs" && id === "system-overrides")
    return <SystemOverrides />;
  if (page === "configs") return <ConfigsPage />;
  return <RunsPage />;
}
