import {
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
  type CSSProperties,
} from "react";
import { WorkspaceSurface, position } from "./surface";
import { WorkspaceToolbar, type TabOption } from "./toolbar";
import {
  panes,
  reconcileLayout,
  updateLayout,
  type Box,
  type Layout,
  type Pane,
} from "./layout";
import {
  WorkspaceId,
  WorkspaceDensity,
  useWorkspaceIds,
  useTabSize,
  type Density,
} from "./context";
import { UnsavedChanges } from "./unsaved-changes";
import { CloseConfirmation } from "./close-confirmation";

export type WorkspaceTab = {
  key: string;
  label: string;
  icon?: ReactNode;
  actions?: ReactNode;
  href?: string;
  scrollKey?: string;
  opener?: string;
  fill?: boolean;
};
export type WorkspaceProps<T extends WorkspaceTab> = {
  density?: Density;
  tabs: readonly T[];
  active?: string;
  onSelect: (key: string) => void;
  onClose: (key: string, next?: string) => void;
  onDuplicate?: (key: string) => string;
  options: readonly TabOption[];
  onOpen: (id: string, opener?: string) => string;
  leading?: ReactNode;
  layout: Layout;
  onLayoutChange: (layout: Layout) => void;
  children: (tab: T, openTabMenu: () => void) => ReactNode;
  empty: (openTabMenu: () => void) => ReactNode;
};

export function Workspace<T extends WorkspaceTab>(props: WorkspaceProps<T>) {
  const id = useId();
  return (
    <WorkspaceId.Provider value={id}>
      <WorkspaceDensity.Provider value={props.density ?? 100}>
        <WorkspaceContent {...props} />
      </WorkspaceDensity.Provider>
    </WorkspaceId.Provider>
  );
}

function WorkspaceContent<T extends WorkspaceTab>({
  density = 100,
  tabs,
  active,
  onSelect,
  onClose,
  onDuplicate,
  options,
  onOpen,
  leading,
  layout,
  onLayoutChange: change,
  children,
  empty,
}: WorkspaceProps<T>) {
  const ids = useWorkspaceIds();
  const { height } = useTabSize();
  const root = useRef<HTMLDivElement>(null);
  const preferredPane = useRef<string | undefined>(undefined);
  const visibleLayout = reconcileLayout(
    layout,
    tabs.map((t) => t.key),
    active,
    preferredPane.current,
  );
  useLayoutEffect(() => {
    if (visibleLayout !== layout) change(visibleLayout);
  }, [visibleLayout, layout]);
  const [menuOpen, setMenuOpen] = useState<string>();
  const [closing, setClosing] = useState<T>();
  const closingFocus = useRef<HTMLElement | null>(null);
  const dirty = useRef(new Map<symbol, string>());
  useEffect(() => {
    const warn = (event: BeforeUnloadEvent) => {
      if (dirty.current.size) event.preventDefault();
    };
    addEventListener("beforeunload", warn);
    return () => removeEventListener("beforeunload", warn);
  }, []);
  function close(key: string) {
    const pane = panes(visibleLayout).find((p) => p.tabs.includes(key));
    if (!pane) return;
    const remaining = pane.tabs.filter((k) => k !== key);
    const opener = tabs.find((tab) => tab.key === key)?.opener;
    const neighbor =
      remaining.find((key) => key === opener) ??
      remaining[Math.min(pane.tabs.indexOf(key), remaining.length - 1)] ??
      tabs.find((t) => t.key !== key)?.key;
    onClose(key, neighbor);
    setClosing(undefined);
    requestAnimationFrame(() =>
      (
        document.getElementById(
          ids.tab(key === pane.active ? (neighbor ?? "") : (pane.active ?? "")),
        ) ??
        root.current?.querySelector<HTMLElement>(
          'button[aria-label="Open a tab"]',
        )
      )?.focus({ preventScroll: true }),
    );
  }
  const openTabMenu = (pane: Pane) => () => {
    preferredPane.current = pane.id;
    setMenuOpen(pane.id);
  };
  return (
    <>
      <div
        ref={root}
        role="region"
        aria-label="Workspace"
        data-density={density}
        style={{ "--lab-tab-height": `${height}px` } as CSSProperties}
        className="lab:flex lab:h-full lab:min-h-0 lab:min-w-0 lab:flex-col lab:overflow-hidden lab:bg-background lab:font-sans lab:text-sm lab:leading-normal lab:font-normal lab:text-foreground"
        onKeyDown={(event) => {
          if (event.key !== "F6") return;
          event.preventDefault();
          const selected = Array.from(
            event.currentTarget.querySelectorAll<HTMLElement>(
              '[role="tab"][aria-selected="true"]',
            ),
          );
          const owner = document.activeElement?.closest(
            '[data-pane-header], [role="tabpanel"]',
          );
          const index = selected.findIndex(
            (tab) =>
              owner?.contains(tab) ||
              tab.getAttribute("aria-controls") === owner?.id,
          );
          selected[
            (index + (event.shiftKey ? selected.length - 1 : 1)) %
              selected.length
          ]?.focus();
        }}
      >
        <WorkspaceSurface
          layout={visibleLayout}
          change={change}
          header={(pane, first, docking, surface) => (
            <WorkspaceToolbar
              leading={first ? leading : undefined}
              docking={docking}
              surface={surface}
              items={pane.tabs.map((key) => tabs.find((t) => t.key === key)!)}
              active={pane.active}
              onSelect={onSelect}
              onDuplicate={
                onDuplicate
                  ? (key) => {
                      const copy = onDuplicate(key),
                        index = pane.tabs.indexOf(key) + 1;
                      change(
                        updateLayout(visibleLayout, pane.id, () => ({
                          ...pane,
                          tabs: [
                            ...pane.tabs.slice(0, index),
                            copy,
                            ...pane.tabs.slice(index),
                          ],
                          active: copy,
                        })),
                      );
                      return copy;
                    }
                  : undefined
              }
              options={options}
              onOpen={(id) => onOpen(id, pane.active)}
              onClose={(key) => {
                if (![...dirty.current.values()].includes(key))
                  return close(key);
                closingFocus.current = document.getElementById(ids.tab(key));
                setClosing(tabs.find((tab) => tab.key === key));
              }}
              menuOpen={menuOpen === pane.id}
              setMenuOpen={(open) => {
                preferredPane.current = pane.id;
                setMenuOpen(open ? pane.id : undefined);
              }}
              onReorder={(keys) =>
                change(
                  updateLayout(visibleLayout, pane.id, (node) => ({
                    ...(node as Pane),
                    tabs: keys,
                  })),
                )
              }
            />
          )}
          content={(regions, transition, lifted) => (
            <>
              {tabs.map((tab) => {
                const region = regions.find((r) =>
                  r.pane.tabs.includes(tab.key),
                )!;
                return (
                  <TabPanel
                    key={tab.key}
                    tab={tab}
                    hidden={
                      tab.key !== region.pane.active && tab.key !== lifted
                    }
                    pane={region.pane.id}
                    box={region.box}
                    transition={transition}
                    focus={() => {
                      preferredPane.current = region.pane.id;
                      if (active !== tab.key) onSelect(tab.key);
                    }}
                  >
                    <UnsavedChanges.Provider
                      value={(source, changed) =>
                        changed
                          ? dirty.current.set(source, tab.key)
                          : dirty.current.delete(source)
                      }
                    >
                      {children(tab, openTabMenu(region.pane))}
                    </UnsavedChanges.Provider>
                  </TabPanel>
                );
              })}
              {!tabs.length && (
                <TabPanel
                  hidden={false}
                  pane={regions[0]!.pane.id}
                  box={regions[0]!.box}
                  transition={transition}
                >
                  {empty(openTabMenu(regions[0]!.pane))}
                </TabPanel>
              )}
            </>
          )}
        />
      </div>
      <CloseConfirmation
        label={closing?.label}
        onCancel={() => setClosing(undefined)}
        onConfirm={() => closing && close(closing.key)}
        restoreFocus={() => {
          if (closingFocus.current?.isConnected) closingFocus.current.focus();
        }}
      />
    </>
  );
}

function TabPanel({
  tab,
  hidden,
  pane,
  box,
  transition,
  focus,
  children,
}: {
  tab?: WorkspaceTab;
  hidden: boolean;
  pane: string;
  box: Box;
  transition: boolean;
  focus?: () => void;
  children: ReactNode;
}) {
  const ids = useWorkspaceIds();
  const { height, borderWidth } = useTabSize();
  const viewport = useRef<HTMLElement>(null);
  useLayoutEffect(() => {
    viewport.current?.scrollTo(0, 0);
  }, [tab?.scrollKey]);
  return (
    <section
      ref={viewport}
      id={tab ? ids.panel(tab.key) : undefined}
      role={tab ? "tabpanel" : undefined}
      aria-labelledby={tab ? ids.tab(tab.key) : undefined}
      tabIndex={0}
      data-pane-sheet={pane}
      hidden={hidden}
      className="lab:transition-[left,top,width,height] lab:duration-180 lab:ease-out lab:motion-reduce:transition-none lab:absolute lab:min-h-0 lab:min-w-0 lab:overflow-auto lab:overscroll-contain lab:rounded-xl lab:border lab:border-border lab:bg-card lab:p-3 lab:outline-none lab:focus-visible:ring-2 lab:focus-visible:ring-ring lab:focus-visible:ring-inset lab:sm:p-5"
      style={{
        borderWidth,
        ...(tab?.fill ? { padding: 0, overflow: "hidden" } : {}),
        ...position({
          ...box,
          y: box.y + height - 1,
          height: box.height - height + 1,
        }),
        transitionDuration: transition
          ? "var(--workspace-duration, 180ms)"
          : "0ms",
      }}
      onPointerDownCapture={focus}
      onFocusCapture={focus}
    >
      {children}
    </section>
  );
}
