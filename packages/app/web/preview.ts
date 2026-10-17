import type {
  Doc,
  Gauge,
  Histogram,
  Operation,
  Receiver,
  Run,
  RunFile,
  Summary,
  Viewer,
  Viewers,
} from "./api";
import type { Inventory } from "./systems/model";

export const readOnly =
  "The preview is read-only. Download RTCBench to run it on your machine.";

type Random = () => number;

function seeded(seed: number): Random {
  let state = seed >>> 0 || 1;
  return () => {
    state = (Math.imul(state, 1664525) + 1013904223) >>> 0;
    return state / 2 ** 32;
  };
}

const between = (random: Random, low: number, high: number) =>
  low + random() * (high - low);
const round = (value: number, digits = 0) => Number(value.toFixed(digits));
const minutes = (iso: string, count: number) =>
  new Date(Date.parse(iso) + count * 60_000).toISOString();
const pad = (value: number, width = 3) => String(value).padStart(width, "0");

type Sample = {
  id: string;
  name: string;
  configName: string;
  plugin: string;
  scenario: string;
  deploymentId?: string;
  roomName?: string;
  envName?: string;
  startedAt: string;
  durationMinutes: number;
  rooms: number;
  viewers: number;
  senders: number;
  status: Run["status"];
  seed: number;
  unhealthy?: number;
  error?: string;
};

const samples: Sample[] = [
  {
    id: "3f1c2a9e-7b64-4c1d-9a8e-5d2b0c7f1e21",
    name: "250 viewers on Jitsi",
    configName: "jitsi/250-viewer-bots",
    plugin: "jitsi",
    scenario: "default",
    envName: "lab",
    startedAt: "2026-10-16T14:02:11.000Z",
    durationMinutes: 12,
    rooms: 25,
    viewers: 250,
    senders: 0,
    status: "completed",
    seed: 11,
  },
  {
    id: "b7e0d4c2-1f3a-4e8b-a6c9-2d5f7a8b9c01",
    name: "Churn on LiveKit",
    configName: "churn",
    plugin: "livekit",
    scenario: "churn",
    deploymentId: "d-livekit-lab",
    roomName: "room-1234",
    envName: "lab",
    startedAt: "2026-10-15T09:30:00.000Z",
    durationMinutes: 30,
    rooms: 1,
    viewers: 5,
    senders: 1,
    status: "completed",
    seed: 23,
  },
  {
    id: "9a4d6e8f-2c3b-4a5d-8e7f-6b1c0d9e2f33",
    name: "Impairment sweep on mediasoup",
    configName: "impairment",
    plugin: "mediasoup",
    scenario: "default",
    envName: "lab",
    startedAt: "2026-10-14T16:45:20.000Z",
    durationMinutes: 9,
    rooms: 10,
    viewers: 100,
    senders: 10,
    status: "failed",
    seed: 37,
    unhealthy: 14,
    error: "run thresholds: 14 of 100 receivers stayed below 20 fps",
  },
  {
    id: "c2d8f1a3-5e6b-4f7c-9d0e-1a2b3c4d5e44",
    name: "Smoke test on Janus",
    configName: "1-camera-5-viewers",
    plugin: "janus",
    scenario: "default",
    startedAt: "2026-10-13T11:12:05.000Z",
    durationMinutes: 3,
    rooms: 1,
    viewers: 5,
    senders: 1,
    status: "completed",
    seed: 41,
  },
  {
    id: "e5f6a7b8-9c0d-4e1f-a2b3-c4d5e6f7a855",
    name: "Tutorial",
    configName: "tutorial",
    plugin: "livekit",
    scenario: "default",
    deploymentId: "d-livekit-lab",
    startedAt: "2026-10-12T18:20:40.000Z",
    durationMinutes: 1,
    rooms: 1,
    viewers: 2,
    senders: 1,
    status: "interrupted",
    seed: 53,
  },
];

const labels = (sample: Sample, role: string) => ({
  Scenario: sample.scenario,
  Plugin: sample.plugin,
  Role: role,
});

function operations(sample: Sample, random: Random): Operation[] {
  const rows: Operation[] = [];
  const add = (
    role: string,
    name: string,
    attempts: number,
    failed: number,
  ) => {
    const mean = between(random, 0.9, 2.4);
    rows.push({
      ...labels(sample, role),
      Operation: name,
      Attempts: attempts,
      Successes: attempts - failed,
      Failures: failed,
      FailureRate: attempts ? failed / attempts : 0,
      MeanSeconds: round(mean, 3),
      P50Seconds: round(mean * 0.9, 3),
      P95Seconds: round(mean * 1.8, 3),
      P99Seconds: round(mean * 2.6, 3),
    });
  };
  const churn = sample.scenario === "churn";
  const joins = churn ? sample.viewers * 60 : sample.viewers;
  add("viewer", "join", joins, churn ? 4 : 0);
  add("viewer", "leave", joins, 0);
  if (sample.senders) {
    add("sender", "join", sample.senders, 0);
    add("sender", "publish_video", sample.senders, 0);
    add("sender", "leave", sample.senders, 0);
  }
  return rows;
}

function sessions(sample: Sample, random: Random): Histogram[] {
  const seconds = sample.durationMinutes * 60;
  const row = (role: string, count: number, mean: number): Histogram => ({
    ...labels(sample, role),
    Name: "rtcbench_session_duration_seconds",
    Count: count,
    MeanSeconds: round(mean, 1),
    MinSeconds: round(mean * between(random, 0.92, 0.99), 1),
    MaxSeconds: round(mean * between(random, 1.01, 1.05), 1),
    P50Seconds: round(mean, 1),
    P95Seconds: round(mean * 1.03, 1),
    P99Seconds: round(mean * 1.04, 1),
  });
  const churn = sample.scenario === "churn";
  return [
    row(
      "viewer",
      churn ? sample.viewers * 60 : sample.viewers,
      churn ? 30 : seconds - 8,
    ),
    ...(sample.senders ? [row("sender", sample.senders, seconds - 5)] : []),
  ];
}

function gauges(sample: Sample): Gauge[] {
  const row = (name: string, peak: number): Gauge => ({
    ...labels(sample, ""),
    Name: name,
    Profile: "",
    UserID: "",
    Current: 0,
    Peak: peak,
  });
  return [
    row("rtcbench_users_active", sample.viewers + sample.senders),
    row("rtcbench_connections_joined", sample.viewers + sample.senders),
    row("rtcbench_publishers_active", sample.senders),
    row("rtcbench_rooms_active", sample.rooms),
  ];
}

function receivers(sample: Sample, random: Random): Receiver[] {
  const rows: Receiver[] = [];
  for (let index = 1; index <= sample.viewers; index++) {
    const degraded = index <= (sample.unhealthy ?? 0);
    const fps = degraded
      ? between(random, 15, 19.5)
      : between(random, 24.3, 25);
    rows.push({
      ...labels(sample, "viewer"),
      Profile: "viewer",
      UserID: `viewer-${pad(index)}`,
      FreezeCount: degraded
        ? Math.floor(between(random, 3, 12))
        : Math.floor(between(random, 0, 1.4)),
      FreezeDurationTotal: round(
        degraded ? between(random, 2, 9) : between(random, 0, 0.4),
        2,
      ),
      FrameLossRatio: round(
        degraded ? between(random, 0.02, 0.08) : between(random, 0, 0.004),
        4,
      ),
      MeanBitrateBps: Math.round(fps * 120_000 * between(random, 0.97, 1.02)),
      MeanFPS: round(fps, 2),
      MaxJitterUS: Math.round(
        between(random, 9_000, degraded ? 90_000 : 45_000),
      ),
      MeanRTTMS: round(between(random, 18, 34), 1),
      MaxRTTMS: round(between(random, 48, 140), 1),
      PliCount: Math.floor(between(random, 0, degraded ? 9 : 2.2)),
      NackCount: Math.floor(between(random, 0, degraded ? 120 : 30)),
    });
  }
  return rows;
}

function summary(sample: Sample): Summary {
  const random = seeded(sample.seed);
  return {
    Operations: operations(sample, random),
    Sessions: sessions(sample, random),
    Gauges: gauges(sample),
    Receivers: receivers(sample, random),
  };
}

function health(sample: Sample): Viewers {
  const random = seeded(sample.seed + 1);
  const seconds = sample.durationMinutes * 60;
  const viewers: Viewer[] = [];
  for (let index = 1; index <= sample.viewers; index++) {
    const degraded = index <= (sample.unhealthy ?? 0);
    const fps = degraded
      ? between(random, 15, 19.5)
      : between(random, 24.3, 25);
    const frames = Math.round(fps * seconds);
    viewers.push({
      nickname: `viewer-${pad(index)}`,
      smooth_bitrate_bps: Math.round(
        fps * 120_000 * between(random, 0.97, 1.02),
      ),
      smooth_fps: round(fps, 2),
      dec_sm_fps: round(fps, 2),
      dec_buf_fps: round(fps - between(random, 0, 0.3), 2),
      est_dec_fps: round(fps, 2),
      frame_jitter_us: Math.round(
        between(random, 3_000, degraded ? 40_000 : 12_000),
      ),
      frames_complete: frames,
      frames_lost: Math.round(
        frames *
          (degraded ? between(random, 0.02, 0.08) : between(random, 0, 0.004)),
      ),
      pli_sent: Math.floor(between(random, 0, degraded ? 9 : 2.2)),
      max_recv_sid: degraded ? 1 : 2,
      max_recv_tid: 2,
      sample_count: Math.round(seconds / 5),
      last_seen_ago_ms: Math.round(between(random, 200, 1800)),
    });
  }
  const total = viewers.reduce((sum, v) => sum + v.smooth_bitrate_bps, 0);
  return {
    status: sample.unhealthy ? "degraded" : "ok",
    viewers_total: sample.viewers,
    viewers_active: sample.status === "completed" ? sample.viewers : 0,
    aggregate_bitrate_mbps: round(total / 1e6, 1),
    errors: sample.unhealthy ? { "fps below threshold": sample.unhealthy } : {},
    viewers,
  };
}

function toRun(sample: Sample): Run {
  const endedAt = minutes(sample.startedAt, sample.durationMinutes);
  const failures = sample.unhealthy
    ? Array.from(
        { length: sample.unhealthy },
        (_, index) => `viewer-${pad(index + 1)}: mean fps below 20`,
      )
    : undefined;
  return {
    id: sample.id,
    name: sample.name,
    configName: sample.configName,
    envName: sample.envName,
    pluginName: sample.plugin,
    deploymentId: sample.deploymentId,
    roomName: sample.roomName,
    status: sample.status,
    startedAt: sample.startedAt,
    endedAt,
    error: sample.error,
    failures,
    summary: summary(sample),
  };
}

function logLines(sample: Sample, stream: string) {
  const random = seeded(sample.seed + 2);
  const at = (offset: number) =>
    new Date(Date.parse(sample.startedAt) + offset * 1000).toISOString();
  const lines: string[] = [];
  const line = (offset: number, level: string, source: string, text: string) =>
    lines.push(`${at(offset)} ${level.padEnd(5)} [${source}] ${text}`);
  const general = stream === "combined" || stream === "general";
  const signaling = stream === "combined" || stream === "signaling";
  const media = stream === "combined" || stream === "media";
  if (general) {
    line(
      0,
      "INFO",
      "orchestrator",
      `loaded ${sample.configName}.yml plugin=${sample.plugin} scenario=${sample.scenario} rooms=${sample.rooms} usersPerRoom=${Math.ceil((sample.viewers + sample.senders) / sample.rooms)}`,
    );
    line(0.2, "INFO", "orchestrator", `plugin ${sample.plugin}: Setup() ok`);
  }
  let offset = 1;
  for (
    let index = 1;
    index <= Math.min(sample.viewers + sample.senders, 12);
    index++
  ) {
    const user =
      index <= sample.senders
        ? `sender-${pad(index)}`
        : `viewer-${pad(index - sample.senders)}`;
    offset += between(random, 0.3, 1.1);
    if (signaling)
      line(
        offset,
        "DEBUG",
        "signaling",
        `${user}: JoinRoom() room=${sample.roomName ?? `room-${pad(Math.ceil(index / Math.max(1, (sample.viewers + sample.senders) / sample.rooms)), 2)}`} ice=completed dtls=connected`,
      );
    if (media)
      line(
        offset + 0.4,
        "INFO",
        "media",
        `${user}: track vp9 ssrc=${Math.floor(between(random, 1e8, 4e9))} ${index <= sample.senders ? "publishing 1080p25 from 1080p25.ivf" : "subscribed, parsing RTP"}`,
      );
  }
  if (general && sample.viewers + sample.senders > 12)
    line(
      offset + 1,
      "INFO",
      "orchestrator",
      `${sample.viewers + sample.senders - 12} more users joined`,
    );
  const seconds = sample.durationMinutes * 60;
  for (
    let tick = 30;
    tick < seconds;
    tick += Math.max(30, Math.floor(seconds / 8))
  ) {
    if (!media) continue;
    const fps =
      sample.unhealthy && tick > seconds / 2
        ? between(random, 21, 23.5)
        : between(random, 24.6, 25);
    line(
      tick,
      "INFO",
      "video_stats",
      `receivers=${sample.viewers} mean_fps=${fps.toFixed(1)} mean_bitrate=${(fps * 0.12).toFixed(2)}Mbps max_jitter=${Math.round(between(random, 9, 40))}ms pli=${Math.floor(between(random, 0, 4))}`,
    );
  }
  if (general) {
    if (sample.status === "failed")
      line(
        seconds - 2,
        "ERROR",
        "thresholds",
        sample.error ?? "thresholds failed",
      );
    if (sample.status === "interrupted")
      line(
        seconds - 1,
        "WARN",
        "orchestrator",
        "interrupt received, leaving rooms",
      );
    line(
      seconds - 0.5,
      "INFO",
      "orchestrator",
      `leaving ${sample.viewers + sample.senders} users`,
    );
    line(
      seconds,
      "INFO",
      "orchestrator",
      `plugin ${sample.plugin}: Shutdown() ok, summary written to metrics/summary.json`,
    );
  }
  return lines.join("\n") + "\n";
}

const streams = ["combined", "general", "signaling", "media"];

function files(sample: Sample, config: string): RunFile[] {
  const run = toRun(sample);
  return [
    { path: "config.yml", size: config.length },
    ...streams.map((stream) => ({
      path: `logs/${stream}.log`,
      size: logLines(sample, stream).length,
    })),
    { path: "metrics/summary.json", size: JSON.stringify(run.summary).length },
    { path: "health.json", size: JSON.stringify(health(sample)).length },
  ];
}

function fileContent(sample: Sample, path: string, config: string) {
  if (path === "config.yml") return config;
  const stream = path.match(/^logs\/(.+)\.log$/)?.[1];
  if (stream && streams.includes(stream)) return logLines(sample, stream);
  if (path === "metrics/summary.json")
    return JSON.stringify(summary(sample), null, 2);
  if (path === "health.json") return JSON.stringify(health(sample), null, 2);
  return undefined;
}

const clusterYaml = `ssh_key: ~/.ssh/id_ed25519
ssh_user: root
sfu: 10.0.0.10
sender: 10.0.0.11
receivers: [10.0.0.12]
senders: [10.0.0.12]
viewer: 10.0.0.11
cores: 0-19
warmup_seconds: 90
steady_state_seconds: 120
sample_seconds: 5
min_bitrate_bps: 2800000
min_fps: 20
healthy_fraction: 1
rtcbench_image: rtcbench:latest
webrtcperf_image: ghcr.io/vpalmisano/webrtcperf:latest
remote_ivf_dir: /opt/ivf-videos
`;

function trial(
  cell: { sfu: string; client: string },
  load: number,
  passed: boolean,
  startedAt: string,
  minute: number,
) {
  const rooms = Math.max(1, Math.round(load / 20));
  return {
    ...cell,
    load,
    repeat: 0,
    repetitions: 1,
    startedAt: minutes(startedAt, minute),
    endedAt: minutes(startedAt, minute + 4),
    passed,
    reason: passed ? "OK" : "QUALITY_DEGRADED",
    topology: {
      loadPerMachine: load,
      rooms,
      limitPerRoom: 20,
      totalSenders: rooms,
      totalReceivers: load,
      expectedStreams: load,
      workers: ["10.0.0.12"],
      observer: "10.0.0.11",
    },
    samples: [0, 1, 2].map((index) => ({
      at: minutes(startedAt, minute + 2 + index),
      host: "10.0.0.12",
      quality: {
        fraction: passed ? 1 : round(0.6 + index * 0.05, 2),
        expected: load,
        healthy: passed ? load : Math.round(load * (0.6 + index * 0.05)),
      },
    })),
    artifacts: `benchmarks/${cell.sfu}-${cell.client}/${load}`,
  };
}

function search(
  cell: { sfu: string; client: string },
  best: number,
  startedAt: string,
) {
  const loads =
    cell.client === "chromium"
      ? [16, 32, 64, 48, 40, 36, best]
      : [500, 1000, 2000, 3000, 2500, 2750, best];
  return loads.map((load, index) =>
    trial(cell, load, load <= best, startedAt, index * 5),
  );
}

const benchmarks = [
  {
    id: "a1f0c3e5-2b4d-4a6c-8e9f-0d1c2b3a4f66",
    name: "LiveKit receivers, RTCBench vs Chromium",
    status: "completed" as const,
    startedAt: "2026-10-11T08:00:00.000Z",
    endedAt: "2026-10-11T09:12:30.000Z",
    request: {
      name: "LiveKit receivers, RTCBench vs Chromium",
      cluster: clusterYaml,
      mode: "receiver",
      cells: [
        { sfu: "livekit", client: "rtcbench" },
        { sfu: "livekit", client: "chromium" },
      ],
      bounds: { min: 1, max: 3000 },
      repetitions: 1,
      provision: true,
      uploadAssets: true,
      dryRun: false,
    },
    report: {
      simulated: false,
      phase: "done",
      results: {
        "livekit/rtcbench": {
          complete: true,
          limit: 3000,
          limitReason: "configured NIC ceiling",
          best: 2512,
          ceilingReached: false,
          attempts: [],
        },
        "livekit/chromium": {
          complete: true,
          limit: 3000,
          limitReason: "configured NIC ceiling",
          best: 37,
          ceilingReached: false,
          attempts: [],
        },
      },
      trials: [
        ...search(
          { sfu: "livekit", client: "rtcbench" },
          2512,
          "2026-10-11T08:00:00.000Z",
        ),
        ...search(
          { sfu: "livekit", client: "chromium" },
          37,
          "2026-10-11T08:36:00.000Z",
        ),
      ],
    },
  },
  {
    id: "b2e1d4f6-3c5e-4b7d-9f0a-1e2d3c4b5a77",
    name: "Senders across SFUs",
    status: "completed" as const,
    startedAt: "2026-10-09T13:15:00.000Z",
    endedAt: "2026-10-09T15:02:10.000Z",
    request: {
      name: "Senders across SFUs",
      cluster: clusterYaml,
      mode: "sender",
      cells: [
        { sfu: "livekit", client: "rtcbench" },
        { sfu: "mediasoup", client: "rtcbench" },
        { sfu: "jitsi", client: "rtcbench" },
      ],
      bounds: { min: 1, max: 2000 },
      repetitions: 1,
      provision: true,
      uploadAssets: true,
      dryRun: false,
    },
    report: {
      simulated: false,
      phase: "done",
      results: {
        "livekit/rtcbench": {
          complete: true,
          limit: 2000,
          limitReason: "search bound",
          best: 1586,
          ceilingReached: false,
          attempts: [],
        },
        "mediasoup/rtcbench": {
          complete: true,
          limit: 2000,
          limitReason: "search bound",
          best: 773,
          ceilingReached: false,
          attempts: [],
        },
        "jitsi/rtcbench": {
          complete: true,
          limit: 2000,
          limitReason: "search bound",
          best: 763,
          ceilingReached: false,
          attempts: [],
        },
      },
      trials: [
        ...search(
          { sfu: "livekit", client: "rtcbench" },
          1586,
          "2026-10-09T13:15:00.000Z",
        ),
        ...search(
          { sfu: "mediasoup", client: "rtcbench" },
          773,
          "2026-10-09T13:50:00.000Z",
        ),
        ...search(
          { sfu: "jitsi", client: "rtcbench" },
          763,
          "2026-10-09T14:25:00.000Z",
        ),
      ],
    },
  },
];

const inventory: Inventory = {
  hosts: [
    {
      id: "h-sfu-1",
      name: "sfu-1",
      address: "192.0.2.10",
      port: 22,
      user: "root",
      fingerprint: "SHA256:4Qm2cXx9fV0u3RkZq8sT1yHn6aWb7cEd5gJi3kLm2oP",
      auth: "file",
      sudo: false,
      keyId: "k-lab",
      networks: [{ networkId: "n-lab", address: "10.0.0.10" }],
    },
    {
      id: "h-load-1",
      name: "load-1",
      address: "192.0.2.11",
      port: 22,
      user: "root",
      fingerprint: "SHA256:9bPz1rMk7dQe4tYw2uIo5pAs8fGh3jKl6zXc0vBn1mN",
      auth: "file",
      sudo: false,
      keyId: "k-lab",
      networks: [{ networkId: "n-lab", address: "10.0.0.11" }],
    },
  ],
  networks: [{ id: "n-lab", name: "lab", cidr: "10.0.0.0/24" }],
  keys: [{ id: "k-lab", name: "lab", path: "~/.ssh/id_ed25519" }],
  deployments: [
    {
      id: "d-livekit-lab",
      name: "livekit-lab",
      plugin: "livekit",
      hostId: "h-sfu-1",
      networkId: "n-lab",
      address: "10.0.0.10",
      status: "ready",
      progress: "",
      ports: { signaling: 7880, rtcTcp: 7881, udpStart: 10000, udpEnd: 60000 },
      logs: "2026-10-12T18:10:02Z pulled livekit/livekit-server:latest\n2026-10-12T18:10:09Z started livekit-lab on 10.0.0.10:7880\n",
    },
    {
      id: "d-jitsi-lab",
      name: "jitsi-lab",
      plugin: "jitsi",
      networkId: "n-lab",
      address: "",
      status: "stopped",
      progress: "",
      ports: { http: 80, signaling: 443, media: 10000 },
      logs: "",
    },
  ],
  plugins: [
    {
      webUI: {},
      id: "livekit",
      name: "LiveKit",
      ports: [
        { id: "signaling", name: "Signaling", protocol: "TCP", default: 7880 },
        { id: "rtcTcp", name: "RTC", protocol: "TCP", default: 7881 },
        {
          id: "udpStart",
          name: "Media range start",
          protocol: "UDP",
          default: 10000,
        },
        {
          id: "udpEnd",
          name: "Media range end",
          protocol: "UDP",
          default: 60000,
          minFrom: "udpStart",
        },
      ],
      credentials: true,
    },
    {
      webUI: {},
      id: "janus",
      name: "Janus",
      ports: [
        { id: "signaling", name: "Signaling", protocol: "TCP", default: 8088 },
        {
          id: "udpStart",
          name: "Media range start",
          protocol: "UDP",
          default: 10000,
        },
        {
          id: "udpEnd",
          name: "Media range end",
          protocol: "UDP",
          default: 20000,
          minFrom: "udpStart",
        },
      ],
    },
    {
      webUI: {},
      id: "mediasoup",
      name: "mediasoup",
      ports: [
        { id: "signaling", name: "Signaling", protocol: "TCP", default: 4443 },
        {
          id: "media",
          name: "First worker",
          protocol: "TCP/UDP",
          default: 44444,
        },
      ],
      portNote:
        "Uses consecutive TCP and UDP ports starting here, one per CPU worker. Keep signaling outside that range.",
    },
    {
      webUI: {},
      id: "jitsi",
      name: "Jitsi",
      ports: [
        { id: "http", name: "HTTP", protocol: "TCP", default: 80 },
        {
          id: "signaling",
          name: "HTTPS / signaling",
          protocol: "TCP",
          default: 443,
        },
        { id: "media", name: "Media", protocol: "UDP", default: 10000 },
      ],
      portNote:
        "Managed deployments reserve internal TCP ports 5222, 5280, 8080, 8888 and 9090.",
    },
  ],
};

const plugins = [
  { id: "janus", name: "Janus", description: "Benchmark Janus VideoRoom" },
  {
    id: "jitsi",
    name: "Jitsi",
    description: "Benchmark Jitsi Meet conferences",
  },
  { id: "livekit", name: "LiveKit", description: "Benchmark LiveKit rooms" },
  {
    id: "mediasoup",
    name: "mediasoup",
    description: "Benchmark mediasoup rooms",
  },
];

const environments: Doc[] = [
  {
    name: "lab",
    yaml: `- name: SERVER_IP
  type: variable
  value: 10.0.0.10
- name: PLUGIN_ID
  type: variable
  value: livekit
- name: LIVEKIT_API_SECRET
  type: secret
`,
    updatedAt: "2026-10-12T18:05:00.000Z",
  },
  {
    name: "cloud",
    yaml: `- name: SERVER_IP
  type: variable
  value: 203.0.113.42
- name: PLUGIN_ID
  type: variable
  value: jitsi
`,
    updatedAt: "2026-10-08T07:40:00.000Z",
  },
];

const json = (data: unknown, status = 200) => Response.json(data, { status });
const text = (data: string) =>
  new Response(data, {
    headers: { "content-type": "text/plain; charset=utf-8" },
  });
const error = (message: string, status = 503) =>
  json({ error: message }, status);
const notFound = () => error("not found", 404);

export function createPreview(base: URL): typeof fetch {
  const docs: Record<string, Map<string, Doc>> = {
    configs: new Map(),
    environments: new Map(environments.map((doc) => [doc.name, doc])),
  };
  let overrides: Doc = {
    name: "system-overrides",
    yaml: "[]\n",
    updatedAt: "2026-10-12T18:05:00.000Z",
  };
  let examples: Promise<string[]> | undefined;
  const loadExamples = () =>
    (examples ??= (async () => {
      const files: string[] = await (
        await fetch(new URL("examples/index.json", base))
      ).json();
      await Promise.all(
        files.map(async (file) => {
          const yaml = await (
            await fetch(new URL(`examples/${file}`, base))
          ).text();
          const name = file.replace(/\.yml$/, "");
          docs.configs!.set(name, {
            name,
            yaml,
            updatedAt: "2026-10-12T18:00:00.000Z",
          });
        }),
      );
      return files;
    })());
  const list = (kind: string) =>
    [...docs[kind]!.values()].sort((a, b) => a.name.localeCompare(b.name));
  const config = async (sample: Sample) => {
    await loadExamples();
    return docs.configs!.get(sample.configName)?.yaml ?? "";
  };

  const handle = async (
    method: string,
    path: string,
    body: () => Promise<unknown>,
  ): Promise<Response> => {
    const segments = path.split("/").filter(Boolean).map(decodeURIComponent);
    const [head, id, ...rest] = segments;
    if (head === "version") return json({ version: "preview" });
    if (head === "examples") {
      const files = await loadExamples();
      if (method === "GET")
        return json({ pattern: "config-examples/**/*.yml", files });
      return new Response(null, { status: 204 });
    }
    if (head === "configs" || head === "environments") {
      if (head === "configs") await loadExamples();
      const store = docs[head]!;
      const name = segments.slice(1).join("/");
      if (method === "GET" && !name) return json(list(head));
      if (method === "POST") {
        const doc = (await body()) as { name: string; yaml: string };
        if (!doc?.name) return error("name is required", 400);
        if (store.has(doc.name))
          return error(`${doc.name} already exists`, 409);
        const saved = {
          name: doc.name,
          yaml: doc.yaml ?? "",
          updatedAt: new Date().toISOString(),
        };
        store.set(doc.name, saved);
        return json(saved, 201);
      }
      const existing = store.get(name);
      if (method === "GET") return existing ? json(existing) : notFound();
      if (method === "PUT") {
        const doc = (await body()) as { name?: string; yaml?: string };
        const renamed = doc?.name || name;
        if (renamed !== name && store.has(renamed))
          return error(`${renamed} already exists`, 409);
        const saved = {
          name: renamed,
          yaml: doc?.yaml ?? existing?.yaml ?? "",
          updatedAt: new Date().toISOString(),
        };
        store.delete(name);
        store.set(renamed, saved);
        return json(saved);
      }
      if (method === "DELETE") {
        store.delete(name);
        return new Response(null, { status: 204 });
      }
    }
    if (head === "system-overrides") {
      if (method === "GET") return json(overrides);
      if (method === "PUT") {
        const doc = (await body()) as { yaml?: string };
        overrides = {
          ...overrides,
          yaml: doc?.yaml ?? overrides.yaml,
          updatedAt: new Date().toISOString(),
        };
        return json(overrides);
      }
    }
    if (head === "runs") {
      if (method !== "GET") return error(readOnly);
      if (!id) return json(samples.map(toRun));
      const sample = samples.find((s) => s.id === id);
      if (!sample) return notFound();
      const [section, ...file] = rest;
      if (!section) return json(toRun(sample));
      if (section === "viewers") return json(health(sample));
      if (section === "files" && file.length === 0)
        return json(files(sample, await config(sample)));
      if (section === "files") {
        const content = fileContent(
          sample,
          file.join("/"),
          await config(sample),
        );
        return content === undefined ? notFound() : text(content);
      }
      if (section === "logs" && streams.includes(file[0] ?? ""))
        return text(logLines(sample, file[0]!));
      return notFound();
    }
    if (head === "benchmarks") {
      if (method !== "GET") return error(readOnly);
      if (!id) return json(benchmarks);
      const benchmark = benchmarks.find((b) => b.id === id);
      if (!benchmark) return notFound();
      if (rest[0] === "files") return rest.length === 1 ? json([]) : notFound();
      return json(benchmark);
    }
    if (head === "systems") {
      if (method !== "GET") return error(readOnly);
      return json(inventory);
    }
    if (head === "deployments") return error(readOnly);
    if (head === "plugins") {
      if (method !== "GET") return error(readOnly);
      if (!id) return json(plugins);
      return error(
        "Plugin projects live in the data directory of a running rtcbench.",
        404,
      );
    }
    if (head === "terminal") return error("Terminals need a running rtcbench.");
    return notFound();
  };

  const preview: typeof fetch = Object.assign(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const request = new Request(input, init);
      const url = new URL(request.url);
      const index = url.pathname.indexOf("/api/v1/");
      if (index < 0) return fetch(request);
      const path = url.pathname.slice(index + "/api/v1".length);
      try {
        return await handle(request.method, path, () => request.json());
      } catch (err) {
        return error((err as Error).message, 500);
      }
    },
    { preconnect() {} },
  );
  return preview;
}
