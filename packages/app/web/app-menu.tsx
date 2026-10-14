import { AppMenu as Menu } from "@labpane/ui";
import type { WorkspaceDensity } from "@labpane/os";
import { usePoll } from "./api";
import { Logo } from "./brand";

export function AppMenu({
  dark,
  onThemeChange,
  density,
  onDensityChange,
  onDensityReset,
  fullscreen,
  onFullscreenChange,
  align,
}: {
  dark: boolean;
  onThemeChange?: (dark: boolean) => void;
  density: WorkspaceDensity;
  onDensityChange?: (density: WorkspaceDensity) => void;
  onDensityReset?: () => void;
  fullscreen?: boolean;
  onFullscreenChange?: (fullscreen: boolean) => void;
  align?: "start" | "end";
}) {
  const { data } = usePoll<{ version: string }>("/version");
  return (
    <Menu
      name="RTCBench"
      logo={<Logo className="labui:size-full" />}
      version={data?.version ?? "Unavailable"}
      dark={dark}
      onThemeChange={onThemeChange}
      size={density}
      onSizeChange={onDensityChange}
      onSizeReset={onDensityReset}
      fullscreen={fullscreen}
      onFullscreenChange={onFullscreenChange}
      align={align}
    />
  );
}
