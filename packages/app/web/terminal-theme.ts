import { useSyncExternalStore } from "react";
import type { TerminalThemeName } from "@labpane/ui/terminal";

const key = "rtcbench:terminal-theme";
export const terminalThemeNames: TerminalThemeName[] = [
  "auto",
  "dark",
  "light",
  "solarized",
  "monokai",
];
const listeners = new Set<() => void>();
let current = read();

function read(): TerminalThemeName {
  try {
    const saved = localStorage.getItem(key);
    return terminalThemeNames.find((name) => name === saved) ?? "auto";
  } catch {
    return "auto";
  }
}

export function setTerminalTheme(theme: TerminalThemeName) {
  current = theme;
  try {
    localStorage.setItem(key, theme);
  } catch {}
  for (const listener of listeners) listener();
}

// useTerminalTheme shares one terminal theme between every terminal tab
export const useTerminalTheme = () =>
  useSyncExternalStore(
    (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    () => current,
  );
