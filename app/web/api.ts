import { useCallback, useEffect, useState } from "react";
import { parse as parseYaml, parseDocument, stringify } from "yaml";

export type DocKind = "config" | "environment";

export type Doc = { name: string; yaml: string; updatedAt: string };

export function parseConfig(yaml: string) {
  const doc = parseDocument(yaml);
  const spec = (...path: string[]) => doc.getIn(["spec", ...path]) ?? null;
  return {
    errors: doc.errors.map((e) => e.message),
    summary: {
      plugin: spec("plugin"),
      scenario: spec("scenario"),
      rooms: spec("conference", "totalRooms"),
      usersPerRoom: spec("conference", "usersPerRoom"),
      camerasPerRoom: spec("conference", "cameras", "perRoom"),
      serverIP: spec("network", "serverIP"),
    },
  };
}

const reserved = /^(con|prn|aux|nul|(com|lpt)[0-9¹²³])(\.|$)/i;

export const validName = (name: string) =>
  [...name].length <= 64 &&
  !/[<>:"\\|?*\p{Cc}]/u.test(name) &&
  name
    .split("/")
    .every(
      (part) =>
        part !== "" &&
        part === part.trim() &&
        !part.startsWith(".") &&
        !part.endsWith(".") &&
        !reserved.test(part),
    );

export type RunStatus = "running" | "completed" | "failed" | "interrupted";

type Labels = { Scenario: string; Plugin: string; Role: string };

export type Operation = Labels & {
  Operation: string;
  Attempts: number;
  Successes: number;
  Failures: number;
  FailureRate: number;
  MeanSeconds: number;
  P50Seconds: number;
  P95Seconds: number;
  P99Seconds: number;
};

export type Histogram = Labels & {
  Name: string;
  Count: number;
  MeanSeconds: number;
  MinSeconds: number;
  MaxSeconds: number;
  P50Seconds: number;
  P95Seconds: number;
  P99Seconds: number;
};

export type Gauge = Labels & {
  Name: string;
  Profile: string;
  UserID: string;
  Current: number;
  Peak: number;
};

export type Receiver = Labels & {
  Profile: string;
  UserID: string;
  FreezeCount: number;
  FreezeDurationTotal: number;
  FrameLossRatio: number;
  MeanBitrateBps: number;
  MeanFPS: number;
  MaxJitterUS: number;
  MeanRTTMS: number;
  MaxRTTMS: number;
  PliCount: number;
  NackCount: number;
};

export type Summary = {
  Operations: Operation[] | null;
  Sessions: Histogram[] | null;
  Gauges: Gauge[] | null;
  Receivers: Receiver[] | null;
};

export type Run = {
  id: string;
  name: string;
  configName?: string;
  envName?: string;
  status: RunStatus;
  startedAt: string;
  endedAt?: string;
  startAt?: number;
  cpuProfile?: boolean;
  env?: Record<string, string>;
  error?: string;
  failures?: string[];
  summary?: Summary;
};

export type Viewer = {
  nickname: string;
  smooth_bitrate_bps: number;
  smooth_fps: number;
  dec_sm_fps: number;
  dec_buf_fps: number;
  est_dec_fps: number;
  frame_jitter_us: number;
  frames_complete: number;
  frames_lost: number;
  pli_sent: number;
  max_recv_sid: number;
  max_recv_tid: number;
  sample_count: number;
  last_seen_ago_ms: number;
};

export type Viewers = {
  status: string;
  viewers_total?: number;
  viewers_active?: number;
  aggregate_bitrate_mbps?: number;
  errors?: Record<string, number>;
  viewers: Viewer[];
};

export type RunFile = { path: string; size: number };

export async function request<T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  const res = await fetch(`/api/v1${path}`, {
    method,
    headers:
      body === undefined ? undefined : { "content-type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) {
    const error = (await res.json().catch(() => null)) as {
      error?: string;
    } | null;
    throw new Error(error?.error ?? res.statusText);
  }
  if (res.status === 202 || res.status === 204) return undefined as T;
  return res.headers.get("content-type")?.includes("json")
    ? res.json()
    : (res.text() as Promise<T>);
}

export function usePoll<T>(path: string | null, interval = 0) {
  const [data, setData] = useState<T>();
  const [error, setError] = useState<string>();
  const load = useCallback(async () => {
    if (!path) return;
    try {
      setData(await request<T>("GET", path));
      setError(undefined);
    } catch (err) {
      setError((err as Error).message);
    }
  }, [path]);
  useEffect(() => {
    load();
    if (!interval) return;
    const timer = setInterval(load, interval);
    return () => clearInterval(timer);
  }, [load, interval]);
  return { data, error, reload: load };
}

export function useNow(active: boolean) {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    if (!active) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [active]);
  return now;
}

export function duration(ms: number) {
  const s = Math.max(0, Math.floor(ms / 1000));
  const hh = Math.floor(s / 3600);
  const mm = String(Math.floor((s % 3600) / 60)).padStart(2, "0");
  const ss = String(s % 60).padStart(2, "0");
  return hh ? `${hh}:${mm}:${ss}` : `${mm}:${ss}`;
}

export const elapsed = (run: Run, now: number) =>
  (run.endedAt ? Date.parse(run.endedAt) : now) - Date.parse(run.startedAt);

export const time = (value: number | string) =>
  new Date(value).toLocaleString();

export const show = (value: unknown) =>
  value === null || value === undefined ? "" : String(value);

export type EnvVar = {
  name: string;
  type: "variable" | "secret" | "system";
  value?: string;
};

export function parseEnv(yaml: string): EnvVar[] {
  const vars: unknown = parseYaml(yaml, { schema: "failsafe" });
  return Array.isArray(vars) ? vars : [];
}

export const formatEnv = (vars: EnvVar[]) => stringify(vars);

export const pages = {
  runs: "Runs",
  configs: "Configs",
  environments: "Environments",
};

const pattern =
  /^\/(runs|run|configs|config|environments|environment)(?:\/(.+))?$/;

export function parse(path: string) {
  const match = path.match(pattern);
  if (!match || path.endsWith(".yml")) return null;
  return { page: match[1]!, id: match[2] && decodeURIComponent(match[2]) };
}

const segments = (name: string) =>
  name.split("/").map(encodeURIComponent).join("/");

export const runPath = (id: string) => `/run/${id}`;
export const docPath = (kind: DocKind, name: string) =>
  `/${kind}/${segments(name)}`;
export const docApi = (kind: DocKind, name: string) =>
  `/${kind}s/${segments(name)}`;

export function go(path: string) {
  history.pushState(null, "", path);
  scrollTo(0, 0);
  dispatchEvent(new PopStateEvent("popstate"));
}

function follow(event: MouseEvent) {
  const link = (event.target as Element).closest("a");
  if (
    !link ||
    link.target ||
    event.defaultPrevented ||
    event.button ||
    event.metaKey ||
    event.ctrlKey ||
    event.shiftKey ||
    event.altKey
  )
    return;
  const url = new URL(link.href);
  if (url.origin !== location.origin || !parse(url.pathname)) return;
  event.preventDefault();
  go(url.pathname);
}

export function useRoute() {
  const [path, setPath] = useState(location.pathname);
  useEffect(() => {
    if (!parse(location.pathname)) history.replaceState(null, "", "/runs");
    const update = () => setPath(location.pathname);
    update();
    addEventListener("popstate", update);
    document.addEventListener("click", follow);
    return () => {
      removeEventListener("popstate", update);
      document.removeEventListener("click", follow);
    };
  }, []);
  return parse(path) ?? { page: "runs", id: undefined };
}
