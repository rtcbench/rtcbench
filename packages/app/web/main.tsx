import { useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  densitySize,
  useFullscreen,
  useWindowFrame,
  type WorkspaceDensity,
} from "@labpane/os";
import { RTCBench } from "./app";
import { desktopWindow, setWindowTheme } from "./desktop";
import { parse, routePath, type Route } from "./routes";

const base = new URL(document.baseURI);
const welcome = document.documentElement.dataset.welcome === "true";
const href = (path: string) => new URL(path, base).href;
const readRoute = () => ({
  ...(parse(location.pathname.slice(base.pathname.length)) ?? {
    page: welcome ? "welcome" : "runs",
  }),
  tabKey: history.state?.tabKey as string | undefined,
});

// densityKey holds a workspace size the user picked; the default is not saved so it can change
const densityKey = "rtcbench:workspace-size";

function Standalone() {
  const [route, setRoute] = useState<Route>(readRoute);
  const [density, setDensity] = useState<WorkspaceDensity>(() => {
    try {
      const saved = localStorage.getItem(densityKey);
      if (saved) return densitySize(Number(saved)).value;
    } catch {}
    return 40;
  });
  const changeDensity = (value: WorkspaceDensity) => {
    setDensity(value);
    try {
      localStorage.setItem(densityKey, String(value));
    } catch {}
  };
  const resetDensity = () => {
    setDensity(40);
    try {
      localStorage.removeItem(densityKey);
    } catch {}
  };
  const [dark, setDark] = useState(() => {
    try {
      return localStorage.getItem("rtcbench:theme") === "dark";
    } catch {
      return false;
    }
  });
  useEffect(() => {
    const read = () => setRoute(readRoute());
    addEventListener("popstate", read);
    return () => removeEventListener("popstate", read);
  }, []);
  useEffect(() => {
    document.documentElement.classList.toggle("dark", dark);
    setWindowTheme(dark);
    try {
      localStorage.setItem("rtcbench:theme", dark ? "dark" : "light");
    } catch {}
  }, [dark]);
  const [fullscreen, setFullscreen] = useFullscreen(desktopWindow);
  useWindowFrame(desktopWindow);
  return (
    <RTCBench
      apiUrl={href("api/v1")}
      href={href}
      welcome={welcome}
      route={route}
      storageKey={"rtcbench:tabs:" + base.pathname}
      dark={dark}
      onThemeChange={setDark}
      density={density}
      onDensityChange={changeDensity}
      onDensityReset={resetDensity}
      fullscreen={fullscreen}
      onFullscreenChange={setFullscreen}
      host={desktopWindow}
      onRouteChange={(next, replace) => {
        history[replace ? "replaceState" : "pushState"](
          { tabKey: next.tabKey },
          "",
          href(routePath(next)),
        );
        setRoute(next);
      }}
    />
  );
}

createRoot(document.getElementById("root")!).render(<Standalone />);
