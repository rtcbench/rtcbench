import { useWorkspaceIds, useTabSize, fitTabWidth } from "./context";
import * as React from "react";
import { createPortal } from "react-dom";
import { ActionMenu } from "./action-menu";
import { XIcon, ChevronLeftIcon, ChevronRightIcon } from "lucide-react";
import { clsx } from "clsx";
import { actionClass } from "./controls";
import type { Docking } from "./surface";
import { TabSurface, paneShape, tabShape } from "./frame";
import type { WorkspaceTab } from "./workspace";
import { useDragContent } from "./drag-content";

const motion = { duration: 160, easing: "cubic-bezier(.2,.8,.2,1)" };

export type TabDrag = {
  key: string;
  x: number;
  y: number;
  detached: boolean;
};

export type WorkspaceTabsProps = {
  items: readonly WorkspaceTab[];
  active: string;
  onClose: (key: string) => void;
  onDuplicate?: (key: string) => string;
  onReorder: (keys: string[]) => void;
  onSelect: (key: string) => void;
  dragBoundary: React.RefObject<HTMLElement | null>;
  surface: (key?: string) => HTMLElement | null;
  docking: Docking;
};

export function WorkspaceTabs({
  items,
  active,
  onClose,
  onDuplicate,
  onReorder,
  onSelect,
  dragBoundary,
  surface,
  docking,
}: WorkspaceTabsProps) {
  const ids = useWorkspaceIds();
  const content = useDragContent();
  const density = useTabSize();
  const [available, setAvailable] = React.useState(Infinity);
  const size = {
      ...density,
      width: fitTabWidth(
        density.width,
        available,
        items.length + (docking.insertion === undefined ? 0 : 1),
      ),
    },
    { width: tabWidth, height, contentHeight } = size,
    width = tabWidth + 16;
  const links = items.map(({ key, label }) => [key, label] as const);
  const tabOrder = JSON.stringify(items.map(({ key }) => key));
  const labels = new Map(links);
  const navRef = React.useRef<HTMLDivElement>(null);
  const stripRef = React.useRef<HTMLDivElement>(null);
  const [scroll, setScroll] = React.useState({
    overflow: false,
    left: false,
    right: false,
  });
  const helpId = React.useId();
  const menuFocus = React.useRef<string | undefined>(undefined);
  const drag = React.useRef<
    | (TabDrag & {
        offset: number;
        offsetY: number;
        sheet: DOMRect;
        moved: boolean;
        order: string[];
      })
    | undefined
  >(undefined);
  const [dragging, setDragging] = React.useState<string>();
  const [pointerFocus, setPointerFocus] = React.useState(false);
  const [announcement, announce] = React.useState("");
  const ghostRef = React.useRef<HTMLDivElement>(null);
  const frameRef = React.useRef<SVGSVGElement>(null);
  const maskedSheet = React.useRef<HTMLElement | null>(null);
  const maskedOutline = React.useRef<SVGPathElement | null>(null);
  const restoreFrame = () => {
    maskedSheet.current?.style.removeProperty("border-color");
    maskedOutline.current?.style.removeProperty("visibility");
    maskedSheet.current = null;
    maskedOutline.current = null;
  };
  const settling = React.useRef<Animation | undefined>(undefined);
  const positions = React.useRef({
    order: "",
    tabs: new Map<string, number>(),
  });
  const frame = React.useRef(0);
  const suppressClick = React.useRef(false);
  const tabElement = (key: string) =>
    navRef.current?.querySelector<HTMLElement>(
      `a[data-tab="${CSS.escape(key)}"]`,
    )?.parentElement;
  const reveal = (key = active) => {
    const nav = navRef.current,
      tab = tabElement(key);
    if (!nav || !tab || drag.current?.moved || docking.insertion !== undefined)
      return;
    nav.scrollLeft = Math.max(
      tab.offsetLeft + tab.offsetWidth + 8 - nav.clientWidth,
      Math.min(nav.scrollLeft, tab.offsetLeft - 8),
    );
  };
  const draw = () => {
    const current = drag.current,
      ghost = ghostRef.current,
      nav = navRef.current,
      bounds = dragBoundary.current?.getBoundingClientRect(),
      floating = !!ghost && !!current?.detached,
      tab = (floating ? null : ghost) ?? tabElement(active),
      frame = frameRef.current;
    if (ghost && current?.moved && bounds) {
      const sheet = surface(current.key);
      const preview =
        floating && sheet
          ? content.lift(sheet, ghost, current.sheet)
          : undefined;
      const left = floating ? 8 : bounds.left + 8,
        right = floating ? innerWidth - 8 : bounds.right - 8;
      const x = Math.max(
        left,
        Math.min(current.x - current.offset, right - ghost.offsetWidth),
      );
      const y = floating
        ? Math.max(
            0,
            Math.min(current.y - current.offsetY, innerHeight - height - 8),
          )
        : nav!.getBoundingClientRect().top;
      ghost.style.left = `${x}px`;
      ghost.style.top = `${y}px`;
      if (preview) {
        content.move(x - preview.offset, y + height - 1);
        const left = preview.offset - 8,
          right = preview.width - preview.offset - tabWidth - 8;
        ghost
          .querySelector("svg > path")!
          .setAttribute("d", tabShape(left, right, size)[0]);
        const outline =
          ghost.querySelector<SVGPathElement>("[data-tab-outline]")!;
        outline.style.visibility = "visible";
        outline.setAttribute(
          "d",
          paneShape(left, right, height + preview.height - 1.5, true, size),
        );
      } else content.release();
    }
    restoreFrame();
    if (!tab || !bounds || !frame) {
      if (frame) frame.style.visibility = "hidden";
      return;
    }
    const box = tab.getBoundingClientRect();
    const sheet = surface(),
      sheetBox = sheet?.getBoundingClientRect();
    const attached = Math.abs(box.top - bounds.top) < 0.5 && !!sheet;
    const left = attached ? box.left - 8 - sheetBox!.left : 16,
      right = attached ? sheetBox!.right - box.right - 8 : 16;
    if (ghost && !floating) {
      const [fill, outline] = tabShape(
        Math.max(0, left),
        Math.max(0, right),
        size,
      );
      ghost.querySelector("svg > path")!.setAttribute("d", fill);
      ghost.querySelector("[data-tab-outline]")!.setAttribute("d", outline);
    }
    const stroke = tab.querySelector<SVGPathElement>("[data-tab-outline]")!;
    stroke.style.visibility = attached ? "hidden" : "visible";
    maskedOutline.current = stroke;
    frame.style.visibility = attached ? "visible" : "hidden";
    if (!attached) return;
    sheet!.style.borderColor = "transparent";
    maskedSheet.current = sheet;
    const x = box.left - 8,
      bottom = sheetBox!.bottom - box.top - 0.5;
    const origin = frame.parentElement!.getBoundingClientRect();
    frame.style.left = `${x - origin.left}px`;
    frame.style.top = `${box.top - origin.top}px`;
    const viewport =
        ghost && !floating ? sheetBox! : nav!.getBoundingClientRect(),
      clipLeft = Math.max(-left, viewport.left - x),
      clipRight = Math.min(width + right, viewport.right - x);
    frame.style.clipPath = `polygon(${clipLeft}px 0px, ${clipRight}px 0px, ${clipRight}px ${height - 1}px, ${width + right}px ${height - 1}px, ${width + right}px ${bottom + 1}px, ${-left}px ${bottom + 1}px, ${-left}px ${height - 1}px, ${clipLeft}px ${height - 1}px)`;
    frame
      .querySelector("path")!
      .setAttribute(
        "d",
        paneShape(left, right, bottom, !!ghost && !floating, size) +
          ((!ghost || floating) && clipLeft > 0
            ? ` M0 ${height - 0.5} H${Math.min(width, clipLeft)}`
            : "") +
          ((!ghost || floating) && clipRight < width
            ? ` M${Math.max(0, clipRight)} ${height - 0.5} H${width}`
            : ""),
      );
  };
  const animate = (element: HTMLElement, from: number) => {
    const transform = getComputedStyle(element).transform;
    if (transform !== "none") from += new DOMMatrixReadOnly(transform).m41;
    element.getAnimations().forEach((animation) => animation.cancel());
    if (!matchMedia("(prefers-reduced-motion: reduce)").matches)
      element.animate(
        [{ transform: `translateX(${from}px)` }, { transform: "none" }],
        motion,
      );
  };
  const settle = (key: string) =>
    requestAnimationFrame(() => {
      if (drag.current) return;
      reveal(key);
      const ghost = ghostRef.current,
        slot = tabElement(key);
      if (
        !ghost ||
        !slot ||
        matchMedia("(prefers-reduced-motion: reduce)").matches
      ) {
        setDragging(undefined);
        return;
      }
      const from = ghost.getBoundingClientRect(),
        to = slot.getBoundingClientRect();
      settling.current = ghost.animate(
        [
          { transform: "none" },
          {
            transform: `translate(${to.left - from.left}px,${to.top - from.top}px)`,
          },
        ],
        { ...motion, fill: "forwards" },
      );
      settling.current.onfinish = () => setDragging(undefined);
      latest.current.refresh();
    });
  const finish = (cancel = false) => {
    const current = drag.current;
    if (!current) return;
    drag.current = undefined;
    suppressClick.current = current.moved;
    content.release();
    if (cancel) {
      docking.cancel();
      if (current.moved) onReorder(current.order);
      announce("");
    }
    if (current.moved && (cancel || !docking.drop())) settle(current.key);
    else setDragging(undefined);
  };
  const move = (from: string, to: string) => {
    const order = links.map(([key]) => key),
      index = order.indexOf(to);
    if (from === to || index < 0) return;
    order.splice(order.indexOf(from), 1);
    order.splice(index, 0, from);
    onReorder(order);
    announce(`${labels.get(from)}: position ${index + 1} of ${order.length}`);
  };
  const focusTab = (key: string) =>
    requestAnimationFrame(() => document.getElementById(ids.tab(key))?.focus());
  const split = (key: string, edge: "left" | "right" | "bottom") => {
    announce(
      docking.split(key, edge)
        ? `${labels.get(key)} split ${edge}`
        : "Open another tab or make this pane larger to split it",
    );
    focusTab(key);
  };
  const keyDown = (event: React.KeyboardEvent, key: string, index: number) => {
    if (event.key === "Escape") return finish(true);
    const edges: Record<string, "left" | "right" | "bottom"> = {
      ArrowLeft: "left",
      ArrowRight: "right",
      ArrowDown: "bottom",
    };
    const edge = edges[event.key];
    if (event.altKey && edge) {
      if (event.shiftKey) split(key, edge);
      else if (edge !== "bottom")
        move(key, items[index + (edge === "left" ? -1 : 1)]?.key ?? key);
      else return;
    } else {
      if (event.altKey || event.ctrlKey || event.metaKey) return;
      const targets: Record<string, number> = {
        Home: 0,
        End: items.length - 1,
        ArrowLeft: (index + items.length - 1) % items.length,
        ArrowRight: (index + 1) % items.length,
      };
      const target = targets[event.key];
      if (target !== undefined)
        document.getElementById(ids.tab(items[target]!.key))?.focus();
      else if (event.key === "Delete") onClose(key);
      else if (event.key === " ") onSelect(key);
      else return;
    }
    event.preventDefault();
  };
  const refresh = (resized = false) => {
    cancelAnimationFrame(frame.current);
    const nav = navRef.current!,
      strip = stripRef.current!;
    const trailing =
      strip.parentElement!.nextElementSibling?.getBoundingClientRect().width ??
      0;
    const availableWidth = Math.max(
      0,
      dragBoundary.current!.getBoundingClientRect().right -
        strip.getBoundingClientRect().left -
        trailing -
        4,
    );
    setAvailable(availableWidth);
    if (resized) reveal();
    const count =
      items.length -
      (drag.current?.detached ? 1 : 0) +
      (docking.insertion === undefined ? 0 : 1);
    const overflow = count * tabWidth + 16 > availableWidth + 1,
      left = nav.scrollLeft > 1,
      right = nav.scrollLeft + nav.clientWidth < nav.scrollWidth - 1;
    setScroll((old) =>
      old.overflow === overflow && old.left === left && old.right === right
        ? old
        : { overflow, left, right },
    );
    draw();
    if (
      [surface(), nav, ghostRef.current].some((element) =>
        element
          ?.getAnimations({ subtree: element === nav })
          .some((animation) => animation.playState === "running"),
      )
    )
      frame.current = requestAnimationFrame(() => latest.current.refresh());
  };
  const latest = React.useRef({ refresh, finish });
  React.useLayoutEffect(() => {
    latest.current = { refresh, finish };
    const order = JSON.stringify([tabOrder, docking.insertion]);
    const previous = positions.current;
    positions.current = { order, tabs: new Map() };
    for (const element of navRef.current!
      .children as HTMLCollectionOf<HTMLElement>) {
      const key = element.querySelector("a")?.dataset.tab;
      if (!key) continue;
      const left = element.offsetLeft,
        from = previous.tabs.get(key);
      if (
        previous.order !== order &&
        key !== dragging &&
        from !== undefined &&
        from !== left
      )
        animate(element, from - left);
      positions.current.tabs.set(key, left);
    }
    refresh();
  });
  React.useLayoutEffect(() => reveal(), [active, tabOrder, docking.insertion]);
  React.useLayoutEffect(() => {
    const nav = navRef.current!;
    const resize = () => latest.current.refresh(true),
      scroll = () => latest.current.refresh(),
      blur = () => latest.current.finish(true);
    const wheel = (event: WheelEvent) => {
      if (
        event.ctrlKey ||
        Math.abs(event.deltaX) >= Math.abs(event.deltaY) ||
        nav.scrollWidth <= nav.clientWidth
      )
        return;
      event.preventDefault();
      nav.scrollLeft +=
        event.deltaY *
        (event.deltaMode === 1
          ? 16
          : event.deltaMode === 2
            ? nav.clientWidth
            : 1);
    };
    const observer = new ResizeObserver(resize);
    for (const element of [
      nav,
      stripRef.current,
      dragBoundary.current,
      surface(),
    ])
      if (element) observer.observe(element);
    addEventListener("scroll", scroll, true);
    addEventListener("blur", blur);
    nav.addEventListener("wheel", wheel, { passive: false });
    return () => {
      observer.disconnect();
      removeEventListener("scroll", scroll, true);
      removeEventListener("blur", blur);
      nav.removeEventListener("wheel", wheel);
      cancelAnimationFrame(frame.current);
      restoreFrame();
    };
  }, [active]);
  const scrollButton = (direction: -1 | 1) =>
    scroll.overflow && (
      <button
        type="button"
        className={actionClass + " lab:disabled:opacity-30"}
        style={{ width: Math.min(28, height), height: Math.min(28, height) }}
        aria-label={direction < 0 ? "Scroll tabs left" : "Scroll tabs right"}
        title={direction < 0 ? "Scroll tabs left" : "Scroll tabs right"}
        disabled={direction < 0 ? !scroll.left : !scroll.right}
        onClick={() => {
          const nav = navRef.current!;
          nav.scrollBy({
            left: direction * Math.max(tabWidth, nav.clientWidth - tabWidth),
            behavior: matchMedia("(prefers-reduced-motion: reduce)").matches
              ? "instant"
              : "smooth",
          });
        }}
      >
        {direction < 0 ? (
          <ChevronLeftIcon aria-hidden="true" className="lab:size-4" />
        ) : (
          <ChevronRightIcon aria-hidden="true" className="lab:size-4" />
        )}
      </button>
    );
  return (
    <div
      ref={stripRef}
      className="lab:relative lab:isolate lab:flex lab:items-center lab:min-w-0 lab:max-w-full"
    >
      {scrollButton(-1)}
      <div
        ref={navRef}
        role={links.length ? "tablist" : undefined}
        aria-label={
          links.length ? `Workspace tabs: ${labels.get(active)}` : undefined
        }
        aria-describedby={links.length ? helpId : undefined}
        onKeyDownCapture={() => setPointerFocus(false)}
        onBlurCapture={() => setPointerFocus(false)}
        onPointerDownCapture={() => {
          content.release(false);
          settling.current?.cancel();
          setDragging(undefined);
          drag.current = undefined;
          suppressClick.current = false;
        }}
        onDragStart={(event) => event.preventDefault()}
        onClickCapture={(event) => {
          if (suppressClick.current) {
            event.preventDefault();
            event.stopPropagation();
            suppressClick.current = false;
          }
        }}
        onPointerMove={(event) => {
          const current = drag.current,
            nav = navRef.current,
            x = event.clientX;
          if (!(event.buttons & 1) || !current || !nav) return;
          if (
            !current.moved &&
            Math.hypot(x - current.x, event.clientY - current.y) < 6
          )
            return;
          if (!current.moved && active !== current.key) onSelect(current.key);
          current.moved = true;
          if (!nav.hasPointerCapture(event.pointerId))
            nav.setPointerCapture(event.pointerId);
          setDragging(current.key);
          event.preventDefault();
          current.x = x;
          current.y = event.clientY;
          current.detached = docking.move(current);
          if (!current.detached) {
            const { left, right } = nav.getBoundingClientRect();
            nav.scrollLeft += x < left + 24 ? -16 : x > right - 24 ? 16 : 0;
            const index = Math.max(
              0,
              Math.min(
                items.length - 1,
                Math.floor(
                  (Math.max(left + 8, Math.min(x, right - 8)) -
                    left +
                    nav.scrollLeft -
                    8) /
                    tabWidth,
                ),
              ),
            );
            move(current.key, items[index]!.key);
          }
          refresh();
        }}
        onPointerUp={() => finish()}
        onPointerCancel={() => finish(true)}
        className="lab:relative lab:flex lab:min-w-0 lab:max-w-full lab:select-none lab:items-center lab:overflow-x-auto lab:px-2 lab:[scrollbar-width:none]"
      >
        {links.map(([key, text], index) => (
          <ActionMenu
            key={key}
            onCloseAutoFocus={(event) => {
              event.preventDefault();
              focusTab(menuFocus.current ?? key);
              menuFocus.current = undefined;
            }}
            actions={[
              !!onDuplicate && [
                "Duplicate tab",
                () => {
                  menuFocus.current = onDuplicate!(key);
                },
              ],
              ["Move left", () => move(key, items[index - 1]!.key), !index],
              [
                "Move right",
                () => move(key, items[index + 1]!.key),
                index === items.length - 1,
              ],
              [
                "Move to next pane",
                () => docking.next(key),
                docking.paneCount < 2,
              ],
              null,
              ...(["left", "right", "bottom"] as const).map(
                (edge) =>
                  [
                    `Split ${edge === "bottom" ? "below" : edge}`,
                    () => split(key, edge),
                    items.length < 2,
                  ] as const,
              ),
              null,
              ["Close tab", () => onClose(key)],
            ]}
          >
            <div
              role="presentation"
              className={clsx(
                "lab:group lab:relative lab:shrink-0 lab:transition-[margin-right] lab:duration-160 lab:ease-out lab:motion-reduce:transition-none",
                dragging === key && "lab:opacity-0",
              )}
              style={{
                paddingTop: height - contentHeight,
                marginRight:
                  dragging === key && drag.current?.detached ? -tabWidth : 0,
                marginLeft: docking.insertion === index ? tabWidth : 0,
                zIndex:
                  active === key ? links.length + 1 : links.length - index,
              }}
            >
              <TabSurface
                width={tabWidth}
                selected={active === key}
                keyboardFocus={!pointerFocus}
              />
              <a
                id={ids.tab(key)}
                role="tab"
                tabIndex={active === key ? 0 : -1}
                aria-selected={active === key}
                aria-controls={ids.panel(key)}
                href={items[index]!.href}
                data-tab={key}
                style={{ width: tabWidth, height: contentHeight }}
                onClick={(event) => {
                  const { metaKey, ctrlKey, shiftKey, altKey } = event;
                  if (metaKey || ctrlKey || shiftKey || altKey) return;
                  event.preventDefault();
                  onSelect(key);
                }}
                onFocus={() => onSelect(key)}
                draggable={false}
                title={text}
                aria-keyshortcuts="ArrowLeft ArrowRight Home End Delete Alt+ArrowLeft Alt+ArrowRight Alt+Shift+ArrowLeft Alt+Shift+ArrowRight Alt+Shift+ArrowDown"
                onKeyDown={(event) => keyDown(event, key, index)}
                onPointerDown={(event) => {
                  if (event.button !== 0 || !event.isPrimary) return;
                  if (event.pointerType === "mouse") event.preventDefault();
                  event.currentTarget.setPointerCapture(event.pointerId);
                  event.currentTarget.focus({ preventScroll: true });
                  setPointerFocus(true);
                  const box =
                    event.currentTarget.parentElement!.getBoundingClientRect();
                  drag.current = {
                    key,
                    offset: event.clientX - box.left,
                    offsetY: event.clientY - box.top,
                    sheet: surface()!.getBoundingClientRect(),
                    x: event.clientX,
                    y: event.clientY,
                    detached: false,
                    moved: false,
                    order: links.map(([path]) => path),
                  };
                }}
                className={clsx(
                  "lab:relative lab:isolate lab:flex lab:items-center lab:rounded-t-lg lab:pr-10 lab:pl-5 lab:text-sm lab:outline-none",
                  dragging === key ? "lab:cursor-grabbing" : "lab:touch-none",
                  active === key
                    ? "lab:font-medium lab:text-foreground"
                    : "lab:font-normal lab:text-muted-foreground lab:hover:text-foreground lab:focus-visible:text-foreground",
                )}
              >
                {items[index]!.icon && (
                  <span aria-hidden="true" className="lab:mr-2 lab:shrink-0">
                    {items[index]!.icon}
                  </span>
                )}
                <span className="lab:min-w-0 lab:truncate">{text}</span>
              </a>
              <CloseTab label={text} onClick={() => onClose(key)} />
            </div>
          </ActionMenu>
        ))}
        {docking.insertion === items.length && (
          <div
            aria-hidden="true"
            style={{ width: tabWidth }}
            className="lab:shrink-0"
          />
        )}
      </div>
      {scrollButton(1)}
      <span className="lab:sr-only" aria-live="polite">
        {announcement}
      </span>
      <span id={helpId} className="lab:sr-only">
        Arrow keys switch tabs. Home and End select the first and last tab.
        Delete closes a tab. Alt plus Left or Right reorders. Alt plus Shift and
        Left, Right, or Down splits a pane. F6 moves between panes; Shift plus
        F6 moves back. Right-click or press Shift plus F10 for tab actions
        without dragging.
      </span>
      {active && (
        <svg
          ref={frameRef}
          aria-hidden="true"
          data-pane-frame
          viewBox={`0 0 ${width} ${height}`}
          strokeWidth={size.borderWidth}
          style={{ width, height }}
          className="lab:pointer-events-none lab:absolute lab:z-[42] lab:overflow-visible"
        >
          <path className="lab:fill-none lab:stroke-border" />
        </svg>
      )}
      {dragging &&
        createPortal(
          <div
            ref={ghostRef}
            data-dragged-tab
            aria-hidden="true"
            inert
            style={{
              width: tabWidth,
              height,
              paddingTop: height - contentHeight,
            }}
            className="lab:font-sans lab:leading-normal lab:text-foreground lab:pointer-events-none lab:fixed lab:z-40 lab:select-none"
          >
            <TabSurface selected width={tabWidth} />
            <span
              style={{ height: contentHeight }}
              className="lab:relative lab:flex lab:items-center lab:pr-10 lab:pl-5 lab:text-sm lab:font-medium"
            >
              {items.find((item) => item.key === dragging)?.icon && (
                <span className="lab:mr-2 lab:shrink-0">
                  {items.find((item) => item.key === dragging)!.icon}
                </span>
              )}
              <span className="lab:min-w-0 lab:truncate">
                {labels.get(dragging)}
              </span>
            </span>
            <CloseTab label={labels.get(dragging)!} />
          </div>,
          document.body,
        )}
    </div>
  );
}

function CloseTab({ label, onClick }: { label: string; onClick?: () => void }) {
  const { height, contentHeight } = useTabSize();
  return (
    <button
      type="button"
      tabIndex={-1}
      aria-label={`Close ${label}`}
      title={`Close ${label}`}
      style={{ top: height - contentHeight / 2 }}
      className="lab:absolute lab:right-3 lab:grid lab:size-6 lab:-translate-y-1/2 lab:place-items-center lab:rounded-full lab:text-muted-foreground lab:transition-colors lab:hover:bg-foreground/10 lab:hover:text-foreground lab:focus-visible:bg-foreground/10 lab:focus-visible:outline-2 lab:focus-visible:outline-ring"
      onClick={onClick}
    >
      <XIcon aria-hidden="true" className="lab:size-3.5" strokeWidth={1.75} />
    </button>
  );
}
