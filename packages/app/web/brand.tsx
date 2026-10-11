import logo from "../../../docs/media/logo_icon.svg";
import type * as React from "react";
import { cn } from "@rtcbench/admin";

export function Logo({ className, ...props }: React.ComponentProps<"img">) {
  return (
    <img
      data-slot="logo"
      src={logo}
      alt=""
      draggable={false}
      aria-hidden="true"
      className={cn("size-8 shrink-0", className)}
      {...props}
    />
  );
}
