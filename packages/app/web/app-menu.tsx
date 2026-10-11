import { MoonIcon, SunIcon } from "lucide-react";
import { densitySize, type WorkspaceDensity } from "@rtcbench/lab";
import { Popover } from "radix-ui";
import { useState } from "react";
import { usePoll } from "./api";
import { Button } from "@rtcbench/admin";
import { Logo } from "./brand";

export function AppMenu({
  dark,
  onThemeChange,
  density,
  onDensityChange,
}: {
  dark: boolean;
  onThemeChange?: (dark: boolean) => void;
  density: WorkspaceDensity;
  onDensityChange?: (density: WorkspaceDensity) => void;
}) {
  const { data } = usePoll<{ version: string }>("/version");
  const ThemeIcon = dark ? SunIcon : MoonIcon;
  const mode = densitySize(density);
  const [anchorHeight, setAnchorHeight] = useState(mode.height);
  return (
    <Popover.Root
      onOpenChange={(open) => {
        if (open) setAnchorHeight(mode.height);
      }}
    >
      <Popover.Trigger asChild>
        <button
          type="button"
          aria-label="RTCBench"
          title="RTCBench menu"
          className="group grid size-[var(--lab-tab-height,2.75rem)] shrink-0 cursor-pointer place-items-center rounded-full outline-none focus-visible:outline-2 focus-visible:outline-ring"
        >
          <Logo
            style={{ width: mode.logoSize, height: mode.logoSize }}
            className="transition-[filter,translate] duration-150 motion-safe:group-hover:-translate-y-0.5 group-hover:drop-shadow-[0_3px_2px_#00000035] group-active:translate-y-0 dark:group-hover:drop-shadow-[0_3px_3px_#00000090]"
          />
        </button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          align="start"
          sideOffset={4 + anchorHeight - mode.height}
          collisionPadding={8}
          aria-label="RTCBench menu"
          onInteractOutside={(event) => {
            if (
              event.target instanceof Element &&
              event.target.closest(".workspace-size-popover")
            )
              event.preventDefault();
          }}
          className={`${dark ? "dark " : ""}z-50 ml-1 flex w-max max-h-[var(--radix-popover-content-available-height)] flex-col items-center gap-3 overflow-auto rounded-lg border bg-popover p-4 text-center text-popover-foreground shadow-sm sm:-ml-1`}
        >
          <div className="flex items-center gap-2 text-2xl">
            <Logo className="size-6" />
            <span className="font-display font-bold tracking-tight">
              RTCBench
            </span>
          </div>
          <div className="text-xs text-muted-foreground">
            <span className="font-mono">{data?.version ?? "Unavailable"}</span>
          </div>
          <div className="flex items-center justify-center gap-2">
            {onThemeChange && (
              <Button
                variant="ghost"
                size="icon-sm"
                className="rounded-full border border-border"
                aria-label="Toggle theme"
                aria-pressed={dark}
                onClick={() => onThemeChange(!dark)}
                title="Toggle theme"
              >
                <ThemeIcon className="size-4" />
              </Button>
            )}
            {onDensityChange && (
              <Popover.Root>
                <Popover.Trigger asChild>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className="rounded-full border border-border"
                    aria-label="Workspace size"
                    title="Workspace size"
                  >
                    <svg
                      aria-hidden="true"
                      viewBox="0 0 24 24"
                      className="size-4 [&_rect]:transition-[fill-opacity]"
                    >
                      {[0, 1, 2].map((row) => (
                        <rect
                          key={row}
                          x="4"
                          y={3 + row * 7}
                          width="16"
                          height="4"
                          rx="1"
                          stroke="currentColor"
                          strokeWidth="1.5"
                          fill="currentColor"
                          fillOpacity={Math.max(
                            0,
                            Math.min(1, (mode.value / 100) * 3 - (2 - row)),
                          )}
                        />
                      ))}
                    </svg>
                  </Button>
                </Popover.Trigger>
                <Popover.Portal>
                  <Popover.Content
                    side="bottom"
                    align="center"
                    sideOffset={8}
                    collisionPadding={8}
                    aria-label="Workspace size"
                    className={`${dark ? "dark " : ""}workspace-size-popover z-[60] rounded-xl border bg-popover p-2 shadow-md`}
                  >
                    <div className="workspace-size">
                      <div
                        className="workspace-size-fill"
                        style={{ height: `${mode.value}%` }}
                      />
                      <input
                        type="range"
                        min={0}
                        max={100}
                        step="any"
                        value={mode.value}
                        aria-label="Workspace size"
                        aria-orientation="vertical"
                        aria-valuetext={`${Math.round(mode.value)}%`}
                        onInput={(event) =>
                          onDensityChange(event.currentTarget.valueAsNumber)
                        }
                      />
                    </div>
                    <Popover.Arrow asChild width={16} height={8}>
                      <svg
                        viewBox="0 0 16 8"
                        className="fill-popover stroke-border"
                      >
                        <path d="M0 0 C3 0 5 6 7 7 Q8 8 9 7 C11 6 13 0 16 0" />
                      </svg>
                    </Popover.Arrow>
                  </Popover.Content>
                </Popover.Portal>
              </Popover.Root>
            )}
            <Button
              asChild
              variant="ghost"
              size="icon-sm"
              className="rounded-full border border-border"
            >
              <a
                href="https://github.com/rtcbench/rtcbench"
                target="_blank"
                rel="noopener noreferrer"
                aria-label="Star on GitHub"
                title="Star on GitHub"
              >
                <svg
                  aria-hidden="true"
                  viewBox="0 0 24 24"
                  className="size-4 fill-current"
                >
                  <path d="M12 .3a12 12 0 0 0-3.8 23.4c.6.1.8-.3.8-.6v-2.2c-3.3.7-4-1.4-4-1.4-.5-1.4-1.3-1.8-1.3-1.8-1.1-.7.1-.7.1-.7 1.2.1 1.8 1.2 1.8 1.2 1.1 1.8 2.8 1.3 3.5 1 .1-.8.4-1.3.8-1.6-2.7-.3-5.5-1.3-5.5-5.9 0-1.3.5-2.4 1.2-3.2-.1-.3-.5-1.5.1-3.2 0 0 1-.3 3.3 1.2a11.5 11.5 0 0 1 6 0c2.3-1.5 3.3-1.2 3.3-1.2.6 1.7.2 2.9.1 3.2.8.8 1.2 1.9 1.2 3.2 0 4.6-2.8 5.6-5.5 5.9.4.4.8 1.1.8 2.2v3.3c0 .3.2.7.8.6A12 12 0 0 0 12 .3Z" />
                </svg>
              </a>
            </Button>
          </div>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
