import { PageTab, useSettings } from "./integration";
import {
  useCallback,
  useEffect,
  useState,
  useMemo,
  useRef,
  useContext,
} from "react";
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
  roomName?: string;
  pluginName?: string;
  deploymentId?: string;
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

async function request<T>(
  fetcher: typeof fetch,
  url: string,
  method: string,
  body?: unknown,
): Promise<T> {
  const res = await fetcher(url, {
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

export function useApi() {
  const { apiUrl = "/api/v1", fetch: fetcher = globalThis.fetch } =
    useSettings();
  return useMemo(() => {
    const url = (path: string) => apiUrl.replace(/\/$/, "") + path;
    return {
      url,
      request: <T>(method: string, path: string, body?: unknown) =>
        request<T>(fetcher, url(path), method, body),
    };
  }, [apiUrl, fetcher]);
}

export function usePoll<T>(
  path: string | null,
  interval = 0,
  refreshOnSelect = false,
) {
  const selected = useContext(PageTab)?.selected ?? true;
  const { request } = useApi();
  const pending = useRef(0);
  const [result, setResult] = useState<{
    path: string;
    request: typeof request;
    data?: T;
    error?: string;
  }>();
  const load = useCallback(async () => {
    const version = ++pending.current;
    if (!path) return;
    try {
      const data = await request<T>("GET", path);
      if (version === pending.current) setResult({ path, request, data });
    } catch (err) {
      if (version === pending.current)
        setResult({ path, request, error: (err as Error).message });
    }
  }, [path, request]);
  useEffect(() => {
    load();
    const timer = interval ? setInterval(load, interval) : undefined;
    return () => {
      ++pending.current;
      clearInterval(timer);
    };
  }, [load, interval, refreshOnSelect && selected]);
  const current =
    result?.path === path && result?.request === request ? result : undefined;
  return { data: current?.data, error: current?.error, reload: load };
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

const segments = (name: string) =>
  name.split("/").map(encodeURIComponent).join("/");

export const runPath = (id: string) => `run/${id}`;
export const docPath = (kind: DocKind, name: string) =>
  `${kind}/${segments(name)}`;
export const docApi = (kind: DocKind, name: string) =>
  `/${kind}s/${segments(name)}`;
