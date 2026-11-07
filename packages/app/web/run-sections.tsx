import { useEffect, useRef, useState } from "react";
import { useSettings } from "./integration";
import { DownloadIcon } from "lucide-react";
import {
  type Gauge,
  type Histogram,
  type Operation,
  type Receiver,
  type RunFile,
  type Summary,
  usePoll,
  useApi,
} from "./api";
import {
  column,
  mono,
  num,
  Table,
  type TableColumn,
  Alert,
  AlertDescription,
  AlertTitle,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@labpane/ui";

function LogView({ text }: { text?: string }) {
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
      className="rtc-code-block labui:h-96 labui:overflow-auto labui:break-normal labui:whitespace-pre"
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
    <section className="labui:flex labui:flex-col labui:gap-4">
      <div className="labui:flex labui:items-center labui:justify-between labui:gap-4">
        <h2 className="labui:text-lg labui:font-semibold">Logs</h2>
        <Select value={stream} onValueChange={setStream}>
          <SelectTrigger
            className="labui:w-44"
            size="sm"
            aria-label="Log stream"
          >
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
  const { url } = useApi();
  const { fetch: fetchFile } = useSettings();
  const [error, setError] = useState<string>();
  const columns: TableColumn<RunFile>[] = [
    {
      ...column(
        "path",
        "File",
        (f) => f.path,
        (f) => (
          <a
            className="labui:inline-flex labui:items-center labui:gap-2 labui:font-mono labui:text-link labui:hover:underline"
            href={url(
              `${path}/files/${f.path.split("/").map(encodeURIComponent).join("/")}`,
            )}
            onClick={
              fetchFile
                ? async (event) => {
                    event.preventDefault();
                    setError(undefined);
                    try {
                      const response = await fetchFile(
                        event.currentTarget.href,
                      );
                      if (!response.ok) throw new Error(response.statusText);
                      const link = document.createElement("a");
                      link.href = URL.createObjectURL(await response.blob());
                      link.download = f.path.split("/").at(-1)!;
                      link.click();
                      URL.revokeObjectURL(link.href);
                    } catch (err) {
                      setError((err as Error).message);
                    }
                  }
                : undefined
            }
          >
            <DownloadIcon className="labui:size-3.5" />
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
    <section className="labui:flex labui:flex-col labui:gap-4">
      <h2 className="labui:text-lg labui:font-semibold">Files</h2>
      {error && (
        <Alert variant="destructive">
          <AlertTitle>{error}</AlertTitle>
        </Alert>
      )}
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
    <section className="labui:flex labui:flex-col labui:gap-4">
      <h2 className="labui:text-lg labui:font-semibold">Summary</h2>
      {failures.length > 0 && (
        <Alert variant="destructive">
          <AlertTitle>Thresholds failed</AlertTitle>
          <AlertDescription className="labui:font-mono labui:text-xs">
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
