import { useEffect, useRef } from "react";

export function useDragContent() {
  const generation = useRef(0);
  const current = useRef<
    | {
        element: HTMLElement;
        width: number;
        height: number;
        offset: number;
        inert: boolean;
      }
    | undefined
  >(undefined);
  const release = (animate = true) => {
    const preview = current.current;
    if (!preview) return;
    current.current = undefined;
    const version = ++generation.current;
    const { element, inert } = preview;
    const from = element.getBoundingClientRect();
    element.removeAttribute("data-drag-content");
    element.inert = inert;
    element.style.transition = "none";
    for (const name of ["x", "y", "width", "height"])
      element.style.removeProperty(`--lab-drag-${name}`);
    requestAnimationFrame(() => {
      if (version !== generation.current) return;
      const to = element.getBoundingClientRect();
      element.style.removeProperty("transition");
      if (
        !animate ||
        !element.isConnected ||
        !to.width ||
        matchMedia("(prefers-reduced-motion: reduce)").matches
      )
        return;
      const resized =
        Math.abs(from.width - to.width) > 0.5 ||
        Math.abs(from.height - to.height) > 0.5;
      element.animate(
        [
          {
            transform: `translate(${from.left - to.left}px, ${from.top - to.top}px)`,
            width: `${from.width}px`,
            height: `${from.height}px`,
            filter: "blur(0)",
            opacity: 1,
          },
          {
            filter: resized ? "blur(3px)" : "blur(0)",
            opacity: resized ? 0.85 : 1,
            offset: 0.35,
          },
          {
            transform: "none",
            width: `${to.width}px`,
            height: `${to.height}px`,
            filter: "blur(0)",
            opacity: 1,
          },
        ],
        { duration: 220, easing: "cubic-bezier(.2,.8,.2,1)" },
      );
    });
  };
  useEffect(() => () => release(false), []);
  return {
    release,
    lift(element: HTMLElement, tab: HTMLElement, fallback: DOMRect) {
      if (current.current?.element !== element) {
        release(false);
        generation.current++;
        element.getAnimations().forEach((animation) => animation.cancel());
        const bounds = element.hidden
          ? fallback
          : element.getBoundingClientRect();
        current.current = {
          element,
          width: bounds.width,
          height: bounds.height,
          offset: Math.max(
            8,
            Math.min(
              tab.getBoundingClientRect().left - bounds.left,
              bounds.width - tab.offsetWidth - 8,
            ),
          ),
          inert: element.inert,
        };
        for (const [name, value] of Object.entries({
          width: `${bounds.width}px`,
          height: `${bounds.height}px`,
        }))
          element.style.setProperty(`--lab-drag-${name}`, String(value));
        element.inert = true;
        element.setAttribute("data-drag-content", "");
      }
      return current.current!;
    },
    move(x: number, y: number) {
      const element = current.current?.element;
      element?.style.setProperty("--lab-drag-x", `${x}px`);
      element?.style.setProperty("--lab-drag-y", `${y}px`);
    },
  };
}
