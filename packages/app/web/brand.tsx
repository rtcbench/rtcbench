import type * as React from "react";
import { cn } from "@labpane/ui";

const bars = [
  [23, "#dc3545"],
  [17, "#2b78e4"],
  [28, "#f5c518"],
  [13, "#f28c28"],
  [25.5, "#2ab673"],
] as const;

// Logo snaps its bars to device pixels so it stays sharp at every workspace size and display scale
export function Logo({ className, ...props }: React.ComponentProps<"svg">) {
  return (
    <svg
      data-slot="logo"
      viewBox="0 0 32 32"
      shapeRendering="crispEdges"
      aria-hidden="true"
      className={cn("labui:size-8 labui:shrink-0", className)}
      {...props}
    >
      {bars.map(([width, fill], index) => (
        <rect
          key={fill}
          x="2"
          y={2 + index * 6}
          width={width}
          height="4"
          fill={fill}
        />
      ))}
    </svg>
  );
}
