import { render } from "preact";
import { useEffect, useState } from "react";
import {
  densityPresets,
  densitySize,
  type WorkspaceDensity,
} from "@rtcbench/lab";
import { RTCBench } from "./app";
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

function Standalone() {
  const [route, setRoute] = useState<Route>(readRoute);
  const [density, setDensity] = useState<WorkspaceDensity>(() => {
    try {
      const saved = localStorage.getItem("rtcbench:density");
      return saved && Object.hasOwn(densityPresets, saved)
        ? densityPresets[saved as keyof typeof densityPresets].value
        : densitySize(Number(saved ?? 100)).value;
    } catch {
      return 100;
    }
  });
  useEffect(() => {
    try {
      localStorage.setItem("rtcbench:density", String(density));
    } catch {}
  }, [density]);
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
    try {
      localStorage.setItem("rtcbench:theme", dark ? "dark" : "light");
    } catch {}
  }, [dark]);
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
      onDensityChange={setDensity}
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

render(<Standalone />, document.getElementById("root")!);
