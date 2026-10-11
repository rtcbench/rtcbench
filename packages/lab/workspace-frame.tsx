import {
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
} from "react";

export function WorkspaceFrame({
  expanded,
  change,
  children,
}: {
  expanded: boolean;
  change: (expanded: boolean) => void;
  children: ReactNode;
}) {
  const viewport = useRef<HTMLDivElement>(null);
  const drag = useRef<{ x: number; width: number } | undefined>(undefined);
  const [available, setAvailable] = useState(0);
  const [draft, setDraft] = useState<number>();
  const compact = Math.min(1600, available);
  const width = draft ?? (expanded ? available : compact);
  const cancel = () => {
    drag.current = undefined;
    setDraft(undefined);
  };
  useLayoutEffect(() => {
    const observer = new ResizeObserver(() => {
      setAvailable(viewport.current!.clientWidth);
      cancel();
    });
    observer.observe(viewport.current!);
    addEventListener("blur", cancel);
    return () => {
      observer.disconnect();
      removeEventListener("blur", cancel);
    };
  }, []);
  return (
    <div ref={viewport} className="lab:h-full lab:min-w-0">
      <div
        className="lab:relative lab:mx-auto lab:h-full"
        style={
          {
            width: available ? width : "100%",
            maxWidth: expanded ? undefined : (draft ?? 1600),
            "--workspace-duration": draft === undefined ? undefined : "0ms",
          } as CSSProperties
        }
      >
        {children}
        {available > compact &&
          ["left", "right"].map((side) => (
            <div
              key={side}
              role="separator"
              aria-label={`Resize workspace from ${side}`}
              aria-orientation="vertical"
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={Math.round(
                ((width - compact) / (available - compact)) * 100,
              )}
              aria-valuetext={
                width === compact
                  ? "Centered"
                  : width === available
                    ? "Full width"
                    : "Resizing"
              }
              title="Drag outward for full width or inward to center. Double-click to toggle; arrow keys also resize."
              tabIndex={0}
              className="lab:absolute lab:top-11 lab:bottom-3 lab:z-40 lab:w-6 lab:cursor-col-resize lab:touch-none lab:select-none lab:rounded lab:outline-none lab:focus-visible:ring-2 lab:focus-visible:ring-ring lab:focus-visible:ring-inset"
              style={{ [side]: -24 }}
              onDoubleClick={() => change(!expanded)}
              onPointerDown={(event) => {
                if (event.button !== 0) return;
                event.preventDefault();
                event.currentTarget.focus({ preventScroll: true });
                drag.current = { x: event.clientX, width };
                setDraft(width);
                event.currentTarget.setPointerCapture(event.pointerId);
              }}
              onPointerMove={(event) => {
                if (
                  !drag.current ||
                  !event.currentTarget.hasPointerCapture(event.pointerId)
                )
                  return;
                const next =
                  drag.current.width +
                  (event.clientX - drag.current.x) * (side === "left" ? -2 : 2);
                const snap = Math.min(32, (available - compact) / 4);
                setDraft(
                  next > available - snap
                    ? available
                    : next < compact + snap
                      ? compact
                      : next,
                );
              }}
              onPointerUp={(event) => {
                if (!drag.current) return;
                change(width > (compact + available) / 2);
                cancel();
                event.currentTarget.releasePointerCapture(event.pointerId);
              }}
              onLostPointerCapture={cancel}
              onPointerCancel={cancel}
              onKeyDown={(event) => {
                if (event.key === "Escape" && drag.current) {
                  event.preventDefault();
                  cancel();
                } else if (
                  ["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)
                ) {
                  event.preventDefault();
                  cancel();
                  change(
                    event.key === "End" ||
                      event.key ===
                        (side === "left" ? "ArrowLeft" : "ArrowRight"),
                  );
                }
              }}
            />
          ))}
      </div>
    </div>
  );
}
