import type { WindowHost } from "@labpane/os";

type Host = { postMessage: (message: string) => void };
declare global {
  interface Window {
    chrome?: { webview?: Host };
    webkit?: { messageHandlers?: { external?: Host } };
  }
}

function detect() {
  if (!bridge || typeof bridge.postMessage !== "function") return;
  const os = new URLSearchParams(location.search).get("desktop");
  const valid = (value: string | null) =>
    value === "darwin" || value === "windows" || value === "linux"
      ? value
      : undefined;
  try {
    if (valid(os)) sessionStorage.setItem("rtcbench:desktop", os!);
    return valid(os ?? sessionStorage.getItem("rtcbench:desktop"));
  } catch {
    return valid(os);
  }
}

const bridge =
  window.chrome?.webview ?? window.webkit?.messageHandlers?.external;
// os is Go's name for the OS when the page runs inside the RTCBench desktop window
const os = detect();
// send posts a Wails v2.16 window message; the desktop build pins that version
const send = (message: string) => bridge?.postMessage(message);

// desktopWindow is the RTCBench desktop window, or undefined in a browser
export const desktopWindow: WindowHost | undefined = os
  ? {
      platform:
        os === "darwin" ? "macos" : os === "windows" ? "windows" : "linux",
      minimize: () => send("Wm"),
      toggleMaximize: () => send("Wt"),
      close: () => send("Q"),
      drag: () => send("drag"),
      ...(os !== "darwin" && {
        resize: (edge) => send(`resize:${edge}-resize`),
      }),
      fullscreen: (on) => send(on ? "WF" : "Wf"),
      ...(os === "darwin" && {
        // The desktop build centres the traffic lights on the title bar; see desktop_window_darwin.go
        trafficLights: (center) =>
          send(
            "EE" + JSON.stringify({ name: "traffic-lights", data: [center] }),
          ),
      }),
    }
  : undefined;

// setWindowTheme matches the window frame to the app's theme
export const setWindowTheme = (dark: boolean) => {
  if (os) send(dark ? "WADT" : "WALT");
};
