import { useMemo } from "react";
import {
  Alert,
  AlertDescription,
  AlertTitle,
  column,
  mono,
  num,
  Sparkline,
  Table,
  type TableColumn,
} from "@labpane/ui";
import type { LiveViewer, RunViewers } from "./api";
import { staleAfter } from "./run-stats";

const trendOf = (viewer: LiveViewer, field: 0 | 1) =>
  viewer.history.map((h) => (field === 0 ? h[0] / 1e6 : h[1]));

const viewerColumns = (
  live: boolean,
  now: number,
): TableColumn<LiveViewer>[] => [
  { ...mono("viewer", "Viewer", (v) => v.nickname), rowHeader: true },
  {
    ...column(
      "bitrate",
      "Bitrate",
      (v) => v.smooth_bitrate_bps / 1e6,
      (v) => (
        <span className="labui:inline-flex labui:items-center labui:justify-end labui:gap-2">
          {v.history.length > 1 && (
            <Sparkline
              values={trendOf(v, 0)}
              label={`Bitrate trend for ${v.nickname}`}
              width={64}
              height={18}
            />
          )}
          {(v.smooth_bitrate_bps / 1e6).toFixed(2)}
        </span>
      ),
    ),
    headerDetail: "Mbps",
    numeric: true,
  },
  {
    ...column(
      "fps",
      "FPS",
      (v) => v.smooth_fps,
      (v) => (
        <span className="labui:inline-flex labui:items-center labui:justify-end labui:gap-2">
          {v.history.length > 1 && (
            <Sparkline
              values={trendOf(v, 1)}
              label={`FPS trend for ${v.nickname}`}
              width={64}
              height={18}
            />
          )}
          {v.smooth_fps.toFixed(1)}
        </span>
      ),
    ),
    numeric: true,
  },
  num("dec", "Decoder FPS", (v) => v.dec_sm_fps, 1),
  num("buf", "Buffer FPS", (v) => v.dec_buf_fps, 1),
  num("est", "Est. decoder FPS", (v) => v.est_dec_fps),
  num("jitter", "Jitter", (v) => v.frame_jitter_us / 1000, 1, "ms"),
  {
    ...column(
      "rtt",
      "RTT",
      (v) => v.rtt_ms,
      (v) => (v.rtt_ms ? v.rtt_ms.toFixed(1) : ""),
    ),
    headerDetail: "ms",
    numeric: true,
  },
  num("frames", "Frames", (v) => v.frames_complete),
  num("lost", "Lost", (v) => v.frames_lost),
  num("pli", "PLI", (v) => v.pli_sent),
  column(
    "svc",
    "SVC",
    (v) => v.max_recv_sid * 10 + v.max_recv_tid,
    (v) => (
      <span className="labui:font-mono">
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
          (v: LiveViewer) => Math.max(0, (now - v.updatedAt) / 1000),
          0,
          "s",
        ),
      ]
    : []),
];

export function ViewersSection({
  data,
  live,
  now,
}: {
  data: RunViewers;
  live: boolean;
  now: number;
}) {
  const viewers = data.viewers;
  const errors = Object.entries(data.errors ?? {});
  const columns = useMemo(() => viewerColumns(live, now), [live, now]);
  const stale = live
    ? viewers.filter((v) => now - v.updatedAt > staleAfter).length
    : 0;

  return (
    <section className="labui:flex labui:flex-col labui:gap-4">
      <h2 className="labui:text-lg labui:font-semibold">
        Viewers
        <span className="labui:ml-2 labui:text-sm labui:font-normal labui:text-muted-foreground">
          {viewers.length}
          {stale ? `, ${stale} stale` : ""}
        </span>
      </h2>
      {errors.length > 0 && (
        <Alert variant="warning">
          <AlertTitle>Errors</AlertTitle>
          <AlertDescription className="labui:font-mono labui:text-xs">
            {errors.map(([name, count]) => (
              <span key={name}>
                {name} {count}
              </span>
            ))}
          </AlertDescription>
        </Alert>
      )}
      <Table
        label="Viewers"
        empty={live ? "Waiting for viewers to receive video" : "No viewers"}
        rows={viewers}
        rowKey={(v) => v.nickname}
        defaultSort={{ column: "viewer", direction: "asc" }}
        columns={columns}
        rowHeight={36}
        rowClassName={(v) =>
          live && now - v.updatedAt > staleAfter
            ? "labui:text-muted-foreground"
            : undefined
        }
      />
    </section>
  );
}
