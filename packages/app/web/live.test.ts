import { expect, it } from "bun:test";
import type { ListMessage, Run, RunMessage, Snapshot } from "./api";
import {
  applyListMessage,
  applyRunMessage,
  liveUrl,
  type LiveSocket,
  type LiveStatus,
  openLive,
  thin,
} from "./live";

const run = {
  id: "r1",
  name: "run",
  status: "running",
  startedAt: "2026-10-16T14:02:11.000Z",
} as Run;

const snapshot: Snapshot = {
  series: {
    start: 1000,
    interval: 1000,
    fields: ["t", "viewers", "active", "bitrate"],
    points: [[2000, 1, 1, 1_000_000]],
  },
  viewers: {
    fields: ["bitrate", "fps", "seen"],
    names: ["viewer-001"],
    rows: [[1_000_000, 24.5, 0.3]],
    start: 1000,
    interval: 1000,
    history: [{ o: 0, h: [[1_000_000, 24.5]] }],
  },
};

it("turns a WebSocket URL out of the API URL", () => {
  expect(
    liveUrl((p) => "https://bench.example/app/api/v1" + p, "/runs/live"),
  ).toBe("wss://bench.example/app/api/v1/runs/live");
  expect(liveUrl((p) => "/api/v1" + p, "/runs/r1/live")).toBe(
    "ws://localhost/api/v1/runs/r1/live",
  );
  expect(liveUrl(() => "file:///x", "/runs/live")).toBeUndefined();
});

it("follows a run from its snapshot through ticks to its end", () => {
  let state = applyRunMessage({}, { type: "snapshot", run, ...snapshot });
  expect(state.run?.status).toBe("running");
  expect(state.series?.points).toHaveLength(1);
  expect(state.viewers?.viewers[0]).toMatchObject({
    nickname: "viewer-001",
    smooth_bitrate_bps: 1_000_000,
    smooth_fps: 24.5,
    last_seen_ago_ms: 300,
    history: [[1_000_000, 24.5]],
    offset: 0,
  });

  const tick: RunMessage = {
    type: "tick",
    p: [3000, 2, 2, 3_000_000],
    n: ["viewer-002"],
    v: [
      [1, 2_000_000, 30, 29, 29, 29, 500, 10, 0, 0, 2, 2, 1, 0.1, 0],
      [0, 1_100_000, 25, 24, 24, 24, 500, 800, 20, 1, 0, 2, 2, 0.2, 42],
    ],
    b: [{ i: 1, r: [[1, 2_000_000, 30]] }],
  };
  state = applyRunMessage(state, tick);
  expect(state.series?.points).toEqual([
    [2000, 1, 1, 1_000_000],
    [3000, 2, 2, 3_000_000],
  ]);
  const [first, second] = state.viewers!.viewers;
  expect(second).toMatchObject({
    nickname: "viewer-002",
    smooth_bitrate_bps: 2_000_000,
    smooth_fps: 30,
    frames_lost: 0,
    history: [[2_000_000, 30]],
    offset: 1,
  });
  expect(first).toMatchObject({
    smooth_bitrate_bps: 1_100_000,
    dec_sm_fps: 24,
    frames_complete: 800,
    frames_lost: 20,
    pli_sent: 1,
    max_recv_sid: 0,
    max_recv_tid: 2,
    sample_count: 2,
    rtt_ms: 42,
  });
  expect(second!.rtt_ms).toBe(0);
  expect(first!.history).toEqual([
    [1_000_000, 24.5],
    [1_000_000, 24.5],
  ]);

  state = applyRunMessage(state, {
    type: "tick",
    p: [3000, 2, 2, 3_000_000],
    n: ["viewer-003"],
    v: [[0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]],
  });
  expect(state.series?.points).toHaveLength(2);
  expect(state.viewers?.viewers).toHaveLength(2);
  expect(state.viewers?.viewers[0]?.smooth_fps).toBe(25);
  state = applyRunMessage(state, {
    type: "tick",
    p: [4000, 2, 2, 3_000_000],
    n: ["viewer-002"],
  });
  expect(state.viewers?.viewers.map((v) => v.nickname)).toEqual([
    "viewer-001",
    "viewer-002",
  ]);

  const compacted: RunMessage = {
    type: "tick",
    series: {
      ...snapshot.series,
      interval: 2000,
      points: [[3000, 2, 2, 2_000_000]],
    },
    viewers: {
      ...snapshot.viewers,
      interval: 2000,
      names: ["viewer-001", "viewer-002"],
      rows: [
        [1_100_000, 25, 1],
        [2_000_000, 30, 1],
      ],
      history: [
        { o: 0, h: [[1_050_000, 24.75]] },
        { o: 0, h: [[2_000_000, 30]] },
      ],
      errors: { "parse vp9 payload": 2 },
    },
  };
  state = applyRunMessage(state, compacted);
  expect(state.series?.interval).toBe(2000);
  expect(state.viewers?.interval).toBe(2000);
  expect(state.viewers?.viewers[1]?.history).toEqual([[2_000_000, 30]]);
  expect(state.viewers?.errors).toEqual({ "parse vp9 payload": 2 });

  state = applyRunMessage(state, { type: "tick", e: {} });
  expect(state.viewers?.errors).toEqual({});

  state = applyRunMessage(state, {
    type: "run",
    run: { ...run, status: "completed" },
  });
  expect(state.run?.status).toBe("completed");
  expect(state.series?.points).toHaveLength(1);
});

it("keeps the runs list and its sparklines in step with the stream", () => {
  const series = {
    start: 0,
    interval: 1000,
    fields: ["t", "bitrate"],
    points: [[1000, 1]],
  };
  let state = applyListMessage(
    { series: {}, runs: [run] },
    { type: "snapshot", series: { r1: series } },
  );
  state = applyListMessage(state, { type: "tick", id: "r1", p: [2000, 3] });
  state = applyListMessage(state, { type: "tick", id: "r2", p: [2000, 3] });
  expect(state.series.r1?.points).toEqual([
    [1000, 1],
    [2000, 3],
  ]);
  expect(state.series.r2).toBeUndefined();
  state = applyListMessage(state, {
    type: "series",
    series: { r2: { ...series, points: [[5000, 9]] } },
  });
  expect(Object.keys(state.series)).toEqual(["r1", "r2"]);
  const started: ListMessage = {
    type: "run",
    run: { ...run, id: "r2", status: "running" },
  };
  state = applyListMessage(state, started);
  expect(state.runs?.map((r) => r.id)).toEqual(["r2", "r1"]);
  state = applyListMessage(state, {
    type: "run",
    run: { ...run, status: "completed" },
  });
  expect(state.runs?.find((r) => r.id === "r1")?.status).toBe("completed");
  state = applyListMessage(state, { type: "deleted", id: "r2" });
  expect(state.runs?.map((r) => r.id)).toEqual(["r1"]);
  expect(applyListMessage(state, { type: "snapshot" }).series).toEqual({});
});

it("thins long sparkline series by merging pairs", () => {
  const points = Array.from({ length: 91 }, (_, i) => [i * 1000, i]);
  const thinned = thin({
    start: 0,
    interval: 1000,
    fields: ["t", "v"],
    points,
  });
  expect(thinned.points).toHaveLength(46);
  expect(thinned.interval).toBe(2000);
  expect(thinned.points[0]).toEqual([1000, 0.5]);
  expect(thinned.points.at(-1)).toEqual([90_000, 90]);
  const short = {
    start: 0,
    interval: 1000,
    fields: ["t", "v"],
    points: points.slice(0, 90),
  };
  expect(thin(short)).toBe(short);
});

class FakeSocket implements LiveSocket {
  static OPEN = 1;
  static instances: FakeSocket[] = [];
  static failing = false;
  readyState = 0;
  sent: string[] = [];
  closedByClient = false;
  onopen: ((event: unknown) => void) | null = null;
  onmessage: ((event: { data: unknown }) => void) | null = null;
  onclose: ((event: { code: number }) => void) | null = null;
  constructor(public url: string) {
    if (FakeSocket.failing) throw new Error("blocked");
    FakeSocket.instances.push(this);
  }
  send(data: string) {
    this.sent.push(data);
  }
  close() {
    this.closedByClient = true;
    this.readyState = 3;
  }
  open() {
    this.readyState = 1;
    this.onopen?.({});
  }
  message(data: unknown) {
    this.onmessage?.({ data });
  }
  drop(code: number) {
    this.readyState = 3;
    this.onclose?.({ code });
  }
}

function harness() {
  FakeSocket.instances = [];
  FakeSocket.failing = false;
  const timers: { fn: () => void; ms: number }[] = [];
  const statuses: LiveStatus[] = [];
  const messages: unknown[] = [];
  const live = openLive<unknown>(
    "ws://bench/api/v1/runs/live",
    { onMessage: (m) => messages.push(m), onStatus: (s) => statuses.push(s) },
    {
      WebSocket: FakeSocket,
      setTimeout: (fn, ms) => {
        const timer = { fn, ms };
        timers.push(timer);
        return timer;
      },
      clearTimeout: (handle) => {
        const i = timers.indexOf(handle as (typeof timers)[number]);
        if (i >= 0) timers.splice(i, 1);
      },
    },
  );
  const fire = () => {
    const timer = timers.shift();
    timer?.fn();
    return timer?.ms;
  };
  return { live, timers, statuses, messages, fire };
}

it("opens a socket, parses messages and only sends while open", () => {
  const { live, statuses, messages } = harness();
  const socket = FakeSocket.instances[0]!;
  expect(socket.url).toBe("ws://bench/api/v1/runs/live");
  live.send({ viewers: false });
  expect(socket.sent).toEqual([]);
  socket.open();
  expect(statuses).toEqual(["connecting", "open"]);
  socket.message('{"type":"tick","id":"r1"}');
  socket.message("not json");
  expect(messages).toEqual([{ type: "tick", id: "r1" }]);
  live.send({ viewers: false });
  expect(socket.sent).toEqual(['{"viewers":false}']);
});

it("reconnects with backoff after a dropped connection, not after a normal close", () => {
  const { live, timers, statuses, fire } = harness();
  FakeSocket.instances[0]!.open();
  FakeSocket.instances[0]!.drop(1006);
  expect(statuses.at(-1)).toBe("closed");
  expect(timers.map((t) => t.ms)).toEqual([1000]);
  fire();
  expect(FakeSocket.instances).toHaveLength(2);
  FakeSocket.instances[1]!.open();
  FakeSocket.instances[1]!.drop(1006);
  expect(fire()).toBe(1000);
  FakeSocket.instances[2]!.drop(1006);
  expect(timers.map((t) => t.ms)).toEqual([60_000]);
  expect(statuses.at(-1)).toBe("unavailable");
  fire();
  FakeSocket.instances[3]!.open();
  FakeSocket.instances[3]!.drop(1000);
  expect(statuses.at(-1)).toBe("closed");
  expect(timers).toEqual([]);
  live.close();
});

it("treats a socket that never opens as unavailable and retries later", () => {
  const { timers, statuses, fire } = harness();
  FakeSocket.instances[0]!.drop(1006);
  expect(statuses).toEqual(["connecting", "unavailable"]);
  expect(timers.map((t) => t.ms)).toEqual([60_000]);
  FakeSocket.failing = true;
  fire();
  expect(statuses.at(-1)).toBe("unavailable");
  expect(timers.map((t) => t.ms)).toEqual([60_000]);
});

it("stops reconnecting and ignores late events once closed", () => {
  const { live, timers, statuses } = harness();
  const socket = FakeSocket.instances[0]!;
  socket.open();
  socket.drop(1006);
  expect(timers).toHaveLength(1);
  live.close();
  expect(timers).toEqual([]);
  socket.drop(1006);
  expect(statuses).toEqual(["connecting", "open", "closed"]);
  expect(FakeSocket.instances).toHaveLength(1);
});
