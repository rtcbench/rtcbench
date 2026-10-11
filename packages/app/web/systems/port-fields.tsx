import type { ReactNode } from "react";
import { TextField } from "@rtcbench/admin";
import type { Plugin } from "./model";

export function PortFields({
  plugin,
  value,
  change,
  disabled,
  children,
}: {
  plugin?: Plugin;
  value: Record<string, number>;
  change: (ports: Record<string, number>) => void;
  disabled?: boolean;
  children?: ReactNode;
}) {
  const ports = {
    ...Object.fromEntries(plugin?.ports.map((p) => [p.id, p.default]) ?? []),
    ...value,
  };
  return (
    <details>
      <summary className="cursor-pointer text-sm font-medium">Ports</summary>
      <fieldset disabled={disabled} className="mt-4 flex flex-col gap-4">
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          {plugin?.ports.map((port) => (
            <div
              key={port.id}
              className={
                plugin.ports.some((p) => p.minFrom === port.id)
                  ? "sm:col-start-1"
                  : undefined
              }
            >
              <TextField
                label={`${port.name} (${port.protocol})`}
                type="number"
                min={port.minFrom ? (ports[port.minFrom] ?? 1) : 1}
                max={65535}
                step={1}
                required
                value={ports[port.id] === 0 ? "" : String(ports[port.id])}
                onChange={(v) => change({ ...value, [port.id]: Number(v) })}
                onInvalid={(event) =>
                  event.currentTarget
                    .closest("details")
                    ?.setAttribute("open", "")
                }
              />
            </div>
          ))}
        </div>
        {plugin?.portNote && (
          <p className="text-sm text-muted-foreground">{plugin.portNote}</p>
        )}
      </fieldset>
      {children}
    </details>
  );
}
