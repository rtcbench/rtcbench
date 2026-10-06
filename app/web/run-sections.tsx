import { useEffect, useRef, useState } from "react";
import { DownloadIcon } from "lucide-react";
import {
  type Gauge,
  type Histogram,
  type Operation,
  type Receiver,
  type RunFile,
  type Summary,
  usePoll,
  type Viewer,
  type Viewers,
} from "./api";
import { column, mono, num, Table, type TableColumn } from "./table";
import {
  Alert,
  AlertDescription,
  AlertTitle,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "./ui";

export function LogView({ text }: { text?: string }) {
  const pre = useRef<HTMLPreElement>(null);
  const pinned = useRef(true);

  useEffect(() => {
    const el = pre.current;
    if (el && pinned.current) el.scrollTop = el.scrollHeight;
  }, [text]);

  return (
    <pre
      ref={pre}
      onScroll={(event) => {
        const el = event.currentTarget;
        pinned.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
      }}
      className="code-block h-96 overflow-auto break-normal whitespace-pre"
    >
      {text}
    </pre>
  );
}

export function LogsSection({
  path,
  files,
  live,
}: {
  path: string;
  files?: RunFile[];
  live: boolean;
}) {
  const [stream, setStream] = useState("combined");
  const streams = (files ?? [])
    .map((f) => f.path.match(/^logs\/(.+)\.log$/)?.[1])
    .filter((s): s is string => !!s);
  const { data } = usePoll<string>(
    streams.includes(stream) ? `${path}/logs/${stream}` : null,
    live ? 2000 : 0,
  );

  return (
    <section className="flex flex-col gap-4">
      <div className="flex items-center justify-between gap-4">
        <h2 className="text-lg font-semibold">Logs</h2>
        <Select value={stream} onValueChange={setStream}>
          <SelectTrigger className="w-44" size="sm" aria-label="Log stream">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {(streams.length ? streams : [stream]).map((s) => (
              <SelectItem key={s} value={s}>
                {s}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <LogView text={data} />
    </section>
  );
}

const size = (bytes: number) =>
  bytes < 1024
    ? `${bytes} B`
    : bytes < 1024 ** 2
      ? `${(bytes / 1024).toFixed(1)} KB`
      : `${(bytes / 1024 ** 2).toFixed(1)} MB`;

export function FilesSection({
  path,
  files,
}: {
  path: string;
  files?: RunFile[];
}) {
  const columns: TableColumn<RunFile>[] = [
    {
      ...column(
        "path",
        "File",
        (f) => f.path,
        (f) => (
          <a
            className="inline-flex items-center gap-2 font-mono text-link hover:underline"
            href={`/api/v1${path}/files/${f.path}`}
          >
            <DownloadIcon className="size-3.5" />
            {f.path}
          </a>
        ),
      ),
      rowHeader: true,
    },
    {
      ...column(
        "size",
        "Size",
        (f) => f.size,
        (f) => size(f.size),
      ),
      numeric: true,
    },
  ];
  return (
    <section className="flex flex-col gap-4">
      <h2 className="text-lg font-semibold">Files</h2>
      <Table
        label="Files"
        rows={files ?? []}
        loading={!files}
        rowKey={(f) => f.path}
        columns={columns}
      />
    </section>
  );
}

const seconds = <T,>(id: string, header: string, value: (row: T) => number) =>
  num(id, header, value, 3, "s");

const operations: TableColumn<Operation>[] = [
  { ...mono("op", "Operation", (o) => o.Operation), rowHeader: true },
  mono("role", "Role", (o) => o.Role),
  num("attempts", "Attempts", (o) => o.Attempts),
  num("successes", "Successes", (o) => o.Successes),
  num("failures", "Failures", (o) => o.Failures),
  num("rate", "Failure rate", (o) => o.FailureRate * 100, 2, "%"),
  seconds("mean", "Mean", (o) => o.MeanSeconds),
  seconds("p50", "p50", (o) => o.P50Seconds),
  seconds("p95", "p95", (o) => o.P95Seconds),
  seconds("p99", "p99", (o) => o.P99Seconds),
];

const sessions: TableColumn<Histogram>[] = [
  { ...mono("name", "Histogram", (h) => h.Name), rowHeader: true },
  mono("role", "Role", (h) => h.Role),
  num("count", "Count", (h) => h.Count),
  seconds("mean", "Mean", (h) => h.MeanSeconds),
  seconds("min", "Min", (h) => h.MinSeconds),
  seconds("max", "Max", (h) => h.MaxSeconds),
  seconds("p50", "p50", (h) => h.P50Seconds),
  seconds("p95", "p95", (h) => h.P95Seconds),
  seconds("p99", "p99", (h) => h.P99Seconds),
];

const gauges: TableColumn<Gauge>[] = [
  { ...mono("name", "Gauge", (g) => g.Name), rowHeader: true },
  mono("role", "Role", (g) => g.Role),
  mono("profile", "Profile", (g) => g.Profile),
  mono("user", "User", (g) => g.UserID),
  num("current", "Current", (g) => g.Current),
  num("peak", "Peak", (g) => g.Peak),
];

const receivers: TableColumn<Receiver>[] = [
  { ...mono("user", "User", (r) => r.UserID), rowHeader: true },
  mono("role", "Role", (r) => r.Role),
  mono("profile", "Profile", (r) => r.Profile),
  num("freezes", "Freezes", (r) => r.FreezeCount),
  num("freeze", "Freeze time", (r) => r.FreezeDurationTotal, 2, "s"),
  num("loss", "Frame loss", (r) => r.FrameLossRatio * 100, 2, "%"),
  num("bitrate", "Bitrate", (r) => r.MeanBitrateBps / 1e6, 2, "Mbps"),
  num("fps", "FPS", (r) => r.MeanFPS, 1),
  num("jitter", "Max jitter", (r) => r.MaxJitterUS / 1000, 1, "ms"),
  num("rtt", "Mean RTT", (r) => r.MeanRTTMS, 1, "ms"),
  num("max-rtt", "Max RTT", (r) => r.MaxRTTMS, 1, "ms"),
  num("pli", "PLI", (r) => r.PliCount),
  num("nack", "NACK", (r) => r.NackCount),
];

function SummaryTable<T>({
  label,
  rows,
  columns,
}: {
  label: string;
  rows: T[] | null;
  columns: TableColumn<T>[];
}) {
  if (!rows?.length) return null;
  return (
    <Table
      label={label}
      rows={rows}
      rowKey={(_, i) => String(i)}
      columns={columns}
    />
  );
}

export function SummarySection({
  summary,
  failures,
}: {
  summary: Summary;
  failures: string[];
}) {
  const tables = [
    summary.Operations,
    summary.Sessions,
    summary.Gauges,
    summary.Receivers,
  ];
  if (!failures.length && !tables.some((rows) => rows?.length)) return null;
  return (
    <section className="flex flex-col gap-4">
      <h2 className="text-lg font-semibold">Summary</h2>
      {failures.length > 0 && (
        <Alert variant="destructive">
          <AlertTitle>Thresholds failed</AlertTitle>
          <AlertDescription className="font-mono text-xs">
            {failures.map((failure) => (
              <span key={failure}>{failure}</span>
            ))}
          </AlertDescription>
        </Alert>
      )}
      <SummaryTable
        label="Operations"
        rows={summary.Operations}
        columns={operations}
      />
      <SummaryTable
        label="Sessions"
        rows={summary.Sessions}
        columns={sessions}
      />
      <SummaryTable label="Gauges" rows={summary.Gauges} columns={gauges} />
      <SummaryTable
        label="Receivers"
        rows={summary.Receivers}
        columns={receivers}
      />
    </section>
  );
}

const columns = (live: boolean): TableColumn<Viewer>[] => [
  { ...mono("viewer", "Viewer", (v) => v.nickname), rowHeader: true },
  num("bitrate", "Bitrate", (v) => v.smooth_bitrate_bps / 1e6, 2, "Mbps"),
  num("fps", "FPS", (v) => v.smooth_fps, 1),
  num("dec", "Decoder FPS", (v) => v.dec_sm_fps, 1),
  num("buf", "Buffer FPS", (v) => v.dec_buf_fps, 1),
  num("est", "Est. decoder FPS", (v) => v.est_dec_fps),
  num("jitter", "Jitter", (v) => v.frame_jitter_us / 1000, 1, "ms"),
  num("frames", "Frames", (v) => v.frames_complete),
  num("lost", "Lost", (v) => v.frames_lost),
  num("pli", "PLI", (v) => v.pli_sent),
  column(
    "svc",
    "SVC",
    (v) => v.max_recv_sid * 10 + v.max_recv_tid,
    (v) => (
      <span className="font-mono">
        S{v.max_recv_sid}T{v.max_recv_tid}
      </span>
    ),
  ),
  num("samples", "Samples", (v) => v.sample_count),
  ...(live
    ? [
        num(
          "seen",
          "Last seen",
          (v: Viewer) => v.last_seen_ago_ms / 1000,
          0,
          "s",
        ),
      ]
    : []),
];

export function ViewersSection({
  data,
  live,
}: {
  data: Viewers;
  live: boolean;
}) {
  const viewers = data.viewers;
  const errors = Object.entries(data.errors ?? {});
  const stats = [
    ["Viewers", data.viewers_total ?? viewers.length],
    ["Active", data.viewers_active ?? 0],
    [
      "Aggregate bitrate",
      `${(data.aggregate_bitrate_mbps ?? 0).toFixed(2)} Mbps`,
    ],
    [
      "Frames lost",
      viewers.reduce((sum, v) => sum + v.frames_lost, 0).toLocaleString(),
    ],
  ] as const;

  return (
    <section className="flex flex-col gap-4">
      <h2 className="text-lg font-semibold">Viewers</h2>
      <dl className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        {stats.map(([label, value]) => (
          <div
            key={label}
            className="flex flex-col gap-1 rounded-lg border bg-card px-4 py-3"
          >
            <dt className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
              {label}
            </dt>
            <dd className="text-xl font-semibold tracking-tight tabular-nums">
              {value}
            </dd>
          </div>
        ))}
      </dl>
      {errors.length > 0 && (
        <Alert variant="warning">
          <AlertTitle>Errors</AlertTitle>
          <AlertDescription className="font-mono text-xs">
            {errors.map(([name, count]) => (
              <span key={name}>
                {name} {count}
              </span>
            ))}
          </AlertDescription>
        </Alert>
      )}
      {viewers.length > 0 && (
        <Table
          label="Viewers"
          rows={viewers}
          rowKey={(v) => v.nickname}
          defaultSort={{ column: "viewer", direction: "asc" }}
          columns={columns(live)}
        />
      )}
    </section>
  );
}
