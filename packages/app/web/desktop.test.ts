import { expect, it } from "bun:test";
import { buildSync } from "esbuild";
import { runInNewContext } from "node:vm";
import { fileURLToPath } from "node:url";
import type { WindowEdge } from "@labpane/os";

const source = buildSync({
  entryPoints: [fileURLToPath(new URL("./desktop.ts", import.meta.url))],
  bundle: true,
  format: "iife",
  globalName: "adapter",
  write: false,
}).outputFiles[0]!.text;

function load(
  search: string,
  transport = "windows",
  saved?: string,
  blocked = false,
) {
  const messages: string[] = [];
  const storage = new Map(saved ? [["rtcbench:desktop", saved]] : []);
  const bridge = {
    postMessage(message: string) {
      messages.push(message);
    },
  };
  const context = {
    window:
      transport === "windows"
        ? { chrome: { webview: bridge } }
        : transport === "webkit"
          ? { webkit: { messageHandlers: { external: bridge } } }
          : {},
    location: { search },
    URLSearchParams,
    sessionStorage: {
      getItem: (key: string) => {
        if (blocked) throw new Error("denied");
        return storage.get(key) ?? null;
      },
      setItem: (key: string, value: string) => {
        if (blocked) throw new Error("denied");
        storage.set(key, value);
      },
    },
  };
  const adapter = runInNewContext(
    source + ";adapter",
    context,
  ) as typeof import("./desktop");
  return { ...adapter, messages, storage };
}

for (const os of ["windows", "linux", "darwin"] as const) {
  it(`sends ${os} actions through its native transport without window.runtime`, () => {
    const {
      desktopWindow: host,
      setWindowTheme,
      messages,
    } = load(`?desktop=${os}`, os === "windows" ? "windows" : "webkit");
    expect(host?.platform).toBe(os === "darwin" ? "macos" : os);
    host!.minimize();
    host!.toggleMaximize();
    host!.close();
    host!.drag!();
    host!.fullscreen!(true);
    host!.fullscreen!(false);
    setWindowTheme(true);
    setWindowTheme(false);
    expect(messages.splice(0)).toEqual([
      "Wm",
      "Wt",
      "Q",
      "drag",
      "WF",
      "Wf",
      "WADT",
      "WALT",
    ]);
    if (os === "darwin") {
      expect(host!.resize).toBeUndefined();
      host!.trafficLights!(23.5);
      expect(messages).toEqual(['EE{"name":"traffic-lights","data":[23.5]}']);
    } else {
      expect(host!.trafficLights).toBeUndefined();
      const edges: WindowEdge[] = ["n", "s", "e", "w", "ne", "nw", "se", "sw"];
      for (const edge of edges) host!.resize!(edge);
      expect(messages).toEqual(edges.map((edge) => `resize:${edge}-resize`));
    }
  });
}

it("requires both a supported platform and a native transport", () => {
  for (const [query, transport, saved] of [
    ["?desktop=windows", "none"],
    ["", "none", "darwin"],
    ["?desktop=unknown", "windows"],
    ["?desktop=", "windows"],
    ["", "windows"],
    ["", "webkit", "unknown"],
  ]) {
    const adapter = load(query!, transport, saved);
    expect(adapter.desktopWindow).toBeUndefined();
    adapter.setWindowTheme(true);
    expect(adapter.messages).toEqual([]);
  }
});

it("restores desktop mode after navigation and tolerates unavailable storage", () => {
  const first = load("?desktop=darwin", "webkit");
  const saved = first.storage.get("rtcbench:desktop");
  expect(saved).toBe("darwin");
  expect(load("", "webkit", saved).desktopWindow?.platform).toBe("macos");
  expect(
    load("?desktop=windows", "windows", undefined, true).desktopWindow
      ?.platform,
  ).toBe("windows");
  expect(load("", "windows", undefined, true).desktopWindow).toBeUndefined();
});
