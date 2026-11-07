import { useContext, useEffect, useMemo, useRef, useState } from "react";
import { fieldIndex } from "@labpane/ui";
import { PageTab, useSettings } from "./integration";
import {
  type ListMessage,
  type LiveUpdate,
  type LiveViewer,
  type Run,
  type RunMessage,
  type RunViewers,
  type Series,
  type Snapshot,
  type ViewersSnapshot,
  useApi,
  usePoll,
} from "./api";

export function liveUrl(url: (path: string) => string, path: string) {
  try {
    const target = new URL(
      url(path),
      typeof document === "undefined" ? "http://localhost/" : document.baseURI,
    );
    if (target.protocol !== "http:" && target.protocol !== "https:")
      return undefined;
    target.protocol = target.protocol === "https:" ? "wss:" : "ws:";
    return target.href;
  } catch {
    return undefined;
  }
}

export type RunLiveState = {
  run?: Run;
  series?: Series;
  viewers?: RunViewers;
};

const viewerFields = [
  "bitrate",
  "fps",
  "dec_fps",
  "buf_fps",
  "est_dec_fps",
  "jitter",
  "frames_complete",
  "frames_lost",
  "pli",
  "sid",
  "tid",
  "samples",
  "seen",
  "rtt",
] as const;

function toViewer(
  name: string,
  row: number[] | undefined,
  index: Record<string, number | undefined>,
  previous?: LiveViewer,
): LiveViewer {
  const at = (field: (typeof viewerFields)[number]) => {
    const i = index[field];
    return i === undefined || !row ? 0 : (row[i] ?? 0);
  };
  return {
    nickname: name,
    smooth_bitrate_bps: at("bitrate"),
    smooth_fps: at("fps"),
    dec_sm_fps: at("dec_fps"),
    dec_buf_fps: at("buf_fps"),
    est_dec_fps: at("est_dec_fps"),
    frame_jitter_us: at("jitter"),
    frames_complete: at("frames_complete"),
    frames_lost: at("frames_lost"),
    pli_sent: at("pli"),
    max_recv_sid: at("sid"),
    max_recv_tid: at("tid"),
    sample_count: at("samples"),
    last_seen_ago_ms: at("seen") * 1000,
    rtt_ms: at("rtt"),
    history: previous?.history ?? [],
    offset: previous?.offset ?? 0,
    updatedAt: Date.now() - at("seen") * 1000,
  };
}

export function fromSnapshot(viewers: ViewersSnapshot): RunViewers {
  const index = fieldIndex(viewers);
  return {
    start: viewers.start,
    interval: viewers.interval,
    errors: viewers.errors,
    viewers: viewers.names.map((name, i) => ({
      ...toViewer(name, viewers.rows[i], index),
      history: viewers.history[i]?.h ?? [],
      offset: viewers.history[i]?.o ?? 0,
    })),
  };
}

export function appendPoint(series: Series, point: number[]): Series {
  const last = series.points.at(-1);
  if (
    last &&
    point[0] !== undefined &&
    last[0] !== undefined &&
    point[0] <= last[0]
  )
    return series;
  return { ...series, points: [...series.points, point] };
}

export function applyUpdate(
  state: RunLiveState,
  update: LiveUpdate,
): RunLiveState {
  let { series, viewers } = state;
  const last = series?.points.at(-1)?.[0];
  if (update.p && last !== undefined && update.p[0]! <= last) return state;
  if (update.series) series = update.series;
  else if (update.p && series) series = appendPoint(series, update.p);
  if (update.viewers) viewers = fromSnapshot(update.viewers);
  else if (viewers && (update.n || update.v || update.b || update.e)) {
    const index = fieldIndex({ fields: viewerFields as unknown as string[] });
    let list = viewers.viewers;
    if (update.n?.length) {
      const known = new Set(list.map((viewer) => viewer.nickname));
      list = [
        ...list,
        ...update.n
          .filter((name) => !known.has(name))
          .map((name) => toViewer(name, undefined, index)),
      ];
    }
    if (update.v?.length) {
      list = [...list];
      for (const row of update.v) {
        const i = row[0]!;
        const previous = list[i];
        if (previous)
          list[i] = toViewer(previous.nickname, row.slice(1), index, previous);
      }
    }
    if (update.b?.length) {
      list = list.map((viewer) => ({
        ...viewer,
        history: [...viewer.history],
      }));
      for (const bucket of update.b) {
        const fresh = new Set<number>();
        for (const [i, bitrate, fps] of bucket.r ?? []) {
          const viewer = list[i];
          if (!viewer) continue;
          if (!viewer.history.length) viewer.offset = bucket.i;
          viewer.history.push([bitrate, fps]);
          fresh.add(i);
        }
        list.forEach((viewer, i) => {
          const last = viewer.history.at(-1);
          if (!fresh.has(i) && last) viewer.history.push(last);
        });
      }
    }
    viewers = {
      ...viewers,
      viewers: list,
      errors: update.e ?? viewers.errors,
    };
  }
  return { run: state.run, series, viewers };
}

export function applyRunMessage(
  state: RunLiveState,
  message: RunMessage,
): RunLiveState {
  switch (message.type) {
    case "snapshot":
      return {
        run: message.run ?? state.run,
        series: message.series,
        viewers: message.viewers ? fromSnapshot(message.viewers) : undefined,
      };
    case "run":
      return { ...state, run: message.run ?? state.run };
    default:
      return applyUpdate(state, message);
  }
}

export type ListLiveState = {
  series: Record<string, Series>;
  runs?: Run[];
};

const sparklineLimit = 90;

export function thin(series: Series): Series {
  if (series.points.length <= sparklineLimit) return series;
  const points: number[][] = [];
  for (let i = 0; i + 1 < series.points.length; i += 2) {
    const [a, b] = [series.points[i]!, series.points[i + 1]!];
    points.push(b.map((value, j) => (j === 0 ? value : (a[j]! + value) / 2)));
  }
  if (series.points.length % 2) points.push(series.points.at(-1)!);
  return { ...series, interval: series.interval * 2, points };
}

export function applyListMessage(
  state: ListLiveState,
  message: ListMessage,
): ListLiveState {
  switch (message.type) {
    case "snapshot":
      return { ...state, series: message.series ?? {} };
    case "series":
      return { ...state, series: { ...state.series, ...message.series } };
    case "tick": {
      const current = message.id ? state.series[message.id] : undefined;
      if (!message.id || !current || !message.p) return state;
      return {
        ...state,
        series: {
          ...state.series,
          [message.id]: thin(appendPoint(current, message.p)),
        },
      };
    }
    case "run": {
      const run = message.run;
      if (!run || !state.runs) return state;
      const runs = state.runs.some((r) => r.id === run.id)
        ? state.runs.map((r) => (r.id === run.id ? run : r))
        : [run, ...state.runs];
      return { ...state, runs };
    }
    case "deleted":
      return state.runs
        ? { ...state, runs: state.runs.filter((r) => r.id !== message.id) }
        : state;
  }
}

export type LiveStatus =
  "idle" | "connecting" | "open" | "closed" | "unavailable";

const retryDelay = (attempt: number) => Math.min(15_000, 1000 * 2 ** attempt);
const unavailableRetry = 60_000;

export type LiveSocket = {
  readyState: number;
  onopen: ((event: unknown) => void) | null;
  onmessage: ((event: { data: unknown }) => void) | null;
  onclose: ((event: { code: number }) => void) | null;
  send(data: string): void;
  close(): void;
};

export type LiveSocketConstructor = (new (url: string) => LiveSocket) & {
  OPEN: number;
};

export type LiveDeps = {
  WebSocket?: LiveSocketConstructor;
  setTimeout?: (fn: () => void, ms: number) => unknown;
  clearTimeout?: (handle: unknown) => void;
};

export type LiveConnection = {
  send: (message: unknown) => void;
  close: () => void;
};

export function openLive<M>(
  target: string,
  handlers: {
    onMessage: (message: M) => void;
    onStatus: (status: LiveStatus) => void;
  },
  deps: LiveDeps = {},
): LiveConnection {
  const Socket =
    deps.WebSocket ??
    (globalThis.WebSocket as unknown as LiveSocketConstructor);
  const later =
    deps.setTimeout ?? ((fn: () => void, ms: number) => setTimeout(fn, ms));
  const cancel =
    deps.clearTimeout ??
    ((handle: unknown) =>
      clearTimeout(handle as ReturnType<typeof setTimeout>));
  let attempt = 0;
  let timer: unknown;
  let closed = false;
  let socket: LiveSocket | undefined;
  const connect = () => {
    let opened = false;
    handlers.onStatus("connecting");
    let ws: LiveSocket;
    try {
      ws = new Socket(target);
    } catch {
      handlers.onStatus("unavailable");
      timer = later(connect, unavailableRetry);
      return;
    }
    socket = ws;
    ws.onopen = () => {
      opened = true;
      attempt = 0;
      handlers.onStatus("open");
    };
    ws.onmessage = (event) => {
      try {
        handlers.onMessage(JSON.parse(String(event.data)) as M);
      } catch {}
    };
    ws.onclose = (event) => {
      if (socket === ws) socket = undefined;
      if (closed) return;
      if (!opened) {
        handlers.onStatus("unavailable");
        timer = later(connect, unavailableRetry);
      } else {
        handlers.onStatus("closed");
        if (event.code !== 1000) timer = later(connect, retryDelay(attempt++));
      }
    };
  };
  connect();
  return {
    send(message) {
      if (socket?.readyState === Socket.OPEN)
        socket.send(JSON.stringify(message));
    },
    close() {
      closed = true;
      cancel(timer);
      socket?.close();
      socket = undefined;
    },
  };
}

export function useLiveSocket<M>(
  path: string | null,
  onMessage: (message: M) => void,
) {
  const { url } = useApi();
  const { fetch: custom } = useSettings();
  const [status, setStatus] = useState<LiveStatus>("idle");
  const connection = useRef<LiveConnection>(undefined);
  const handler = useRef(onMessage);
  handler.current = onMessage;
  const target = path && !custom ? liveUrl(url, path) : undefined;

  useEffect(() => {
    if (!target || typeof WebSocket === "undefined") {
      setStatus(path ? "unavailable" : "idle");
      return;
    }
    const live = openLive<M>(target, {
      onMessage: (message) => handler.current(message),
      onStatus: setStatus,
    });
    connection.current = live;
    return () => {
      live.close();
      connection.current = undefined;
      setStatus("idle");
    };
  }, [target, path]);

  const send = useMemo(
    () => (message: unknown) => connection.current?.send(message),
    [],
  );
  return { status, send };
}

export function useVisible() {
  const selected = useContext(PageTab)?.selected ?? true;
  const [shown, setShown] = useState(
    () => typeof document === "undefined" || !document.hidden,
  );
  useEffect(() => {
    if (typeof document === "undefined") return;
    const update = () => setShown(!document.hidden);
    document.addEventListener("visibilitychange", update);
    return () => document.removeEventListener("visibilitychange", update);
  }, []);
  return selected && shown;
}

export function useRunLive(path: string, run: Run | undefined) {
  const live = run?.status === "running";
  const visible = useVisible();
  const [state, setState] = useState<RunLiveState>({});
  const { status } = useLiveSocket<RunMessage>(
    live && visible ? `${path}/live` : null,
    (message) => setState((previous) => applyRunMessage(previous, message)),
  );
  const streaming = live && status === "open";
  const { data: polled } = usePoll<Snapshot>(
    run && !streaming && (!live || status === "unavailable")
      ? `${path}/series`
      : null,
    live ? 2000 : 0,
  );
  const snapshot = useMemo<RunLiveState>(() => {
    if (polled && (!live || !state.series))
      return { series: polled.series, viewers: fromSnapshot(polled.viewers) };
    return state;
  }, [live, state, polled]);
  return {
    ended: state.run && state.run.status !== "running" ? state.run : undefined,
    series: snapshot.series,
    viewers: snapshot.viewers,
    status,
  };
}

export function useRunsLive(enabled: boolean) {
  const visible = useVisible();
  const [state, setState] = useState<ListLiveState>({ series: {} });
  const { status } = useLiveSocket<ListMessage>(
    enabled && visible ? "/runs/live" : null,
    (message) => setState((previous) => applyListMessage(previous, message)),
  );
  const streaming = status === "open";
  const { data: runs, error } = usePoll<Run[]>(
    "/runs",
    streaming ? 30_000 : 2000,
  );
  useEffect(() => {
    if (runs) setState((previous) => ({ ...previous, runs }));
  }, [runs]);
  const active = !!state.runs?.some((r) => r.status === "running");
  const { data: polledSeries } = usePoll<Record<string, Series>>(
    enabled && visible && !streaming && status !== "connecting"
      ? "/runs/series"
      : null,
    active ? 5000 : 0,
  );
  const series = streaming ? state.series : (polledSeries ?? state.series);
  return { runs: state.runs, series, status, error };
}
