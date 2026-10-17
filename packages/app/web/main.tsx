import { useEffect, useState } from "react";
import { BrowserRouter, useLocation, useNavigate } from "react-router";
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
const { releases, preview } = document.documentElement.dataset;
const persistent = preview !== "true";
const href = (path: string) => new URL(path, base).href;
// densityKey holds a workspace size the user picked; the default is not saved so it can change
const densityKey = "rtcbench:workspace-size";
const previewFetch =
  preview === "true"
    ? (await import("./preview")).createPreview(base)
    : undefined;

function Standalone() {
  const location = useLocation();
  const navigate = useNavigate();
  const route: Route = {
    ...(location.pathname === "/"
      ? { page: welcome ? "welcome" : "runs" }
      : (parse(location.pathname) ?? {
          page: "not-found",
          id: location.pathname.slice(1),
        })),
    tabKey: location.state?.tabKey ?? window.history.state?.tabKey,
  };
  const [density, setDensity] = useState<WorkspaceDensity>(() => {
    try {
      const saved = persistent ? localStorage.getItem(densityKey) : null;
      if (saved) return densitySize(Number(saved)).value;
    } catch {}
    return 40;
  });
  const changeDensity = (value: WorkspaceDensity) => {
    setDensity(value);
    try {
      if (persistent) localStorage.setItem(densityKey, String(value));
    } catch {}
  };
  const resetDensity = () => {
    setDensity(40);
    try {
      if (persistent) localStorage.removeItem(densityKey);
    } catch {}
  };
  const [dark, setDark] = useState(() => {
    try {
      return persistent && localStorage.getItem("rtcbench:theme") === "dark";
    } catch {
      return false;
    }
  });
  useEffect(() => {
    document.documentElement.classList.toggle("dark", dark);
    setWindowTheme(dark);
    try {
      if (persistent)
        localStorage.setItem("rtcbench:theme", dark ? "dark" : "light");
    } catch {}
  }, [dark]);
  const [fullscreen, setFullscreen] = useFullscreen(desktopWindow);
  useWindowFrame(desktopWindow);
  return (
    <RTCBench
      apiUrl={href("api/v1")}
      fetch={previewFetch}
      href={href}
      welcome={welcome}
      releases={releases}
      route={route}
      storageKey={persistent ? "rtcbench:tabs:" + base.pathname : undefined}
      dark={dark}
      onThemeChange={setDark}
      density={density}
      onDensityChange={changeDensity}
      onDensityReset={resetDensity}
      fullscreen={fullscreen}
      onFullscreenChange={setFullscreen}
      host={desktopWindow}
      onRouteChange={(next, replace) => {
        navigate(
          {
            pathname: "/" + routePath(next),
            search: replace ? location.search : "",
            hash: replace ? location.hash : "",
          },
          {
            replace,
            state: { tabKey: next.tabKey },
          },
        );
      }}
    />
  );
}

createRoot(document.getElementById("root")!).render(
  <BrowserRouter basename={base.pathname} useTransitions={false}>
    <Standalone />
  </BrowserRouter>,
);
