import { useMemo } from "react";
import {
  bytesLabel,
  compactNumber as compact,
  fieldIndex,
  rates,
  Sparkline,
  Stat,
  StatDetail,
  StatLabel,
  Stats,
  StatValue,
} from "@labpane/ui";
import type { RunViewers, Series } from "./api";
import { receivedBytes } from "./metrics";

export const staleAfter = 30_000;

type Tile = {
  id: string;
  label: string;
  value: string;
  unit?: string;
  detail?: string;
  trend?: number[];
  tone?: "warning" | "destructive";
};

const trendLength = 60;
const mean = (values: number[]) =>
  values.length ? values.reduce((sum, v) => sum + v, 0) / values.length : 0;

export function trend(values: number[], live: boolean, limit = trendLength) {
  if (live || values.length <= limit) return values.slice(-limit);
  const size = Math.ceil(values.length / limit);
  const out: number[] = [];
  for (let i = 0; i < values.length; i += size)
    out.push(mean(values.slice(i, i + size)));
  return out;
}

export function tiles(
  series: Series | undefined,
  viewers: RunViewers | undefined,
  live: boolean,
  now: number,
): Tile[] {
  const index = series ? fieldIndex(series) : {};
  const points = series?.points ?? [];
  const last = points.at(-1);
  const at = (p: number[] | undefined, field: string) => {
    const i = index[field];
    return i === undefined || !p ? undefined : p[i];
  };
  const col = (field: string) =>
    trend(
      points.map((p) => at(p, field) ?? 0),
      live,
    );
  const list = viewers?.viewers ?? [];
  const stale = live
    ? list.filter((v) => now - v.updatedAt > staleAfter).length
    : 0;
  const joined = at(last, "viewers") ?? list.length;
  const active = live
    ? (at(last, "active") ?? list.length - stale)
    : Math.max(0, ...points.map((p) => at(p, "active") ?? 0));
  const bitrates = col("bitrate").map((v) => v / 1e6);
  const bitrate = live
    ? (bitrates.at(-1) ??
      list.reduce((s, v) => s + v.smooth_bitrate_bps, 0) / 1e6)
    : Math.max(0, ...points.map((p) => (at(p, "bitrate") ?? 0) / 1e6));
  const busy = points.filter((p) => (at(p, "active") ?? 0) > 0);
  const fps = live
    ? (at(last, "fps_p50") ?? mean(list.map((v) => v.smooth_fps)))
    : mean(busy.map((p) => at(p, "fps_p50") ?? 0));
  const fpsLow = live
    ? at(last, "fps_p10")
    : Math.min(...busy.map((p) => at(p, "fps_p10") ?? Infinity));
  const fpsHigh = live
    ? at(last, "fps_p90")
    : Math.max(...busy.map((p) => at(p, "fps_p90") ?? 0));
  const lost =
    at(last, "frames_lost") ?? list.reduce((s, v) => s + v.frames_lost, 0);
  const complete =
    at(last, "frames_complete") ??
    list.reduce((s, v) => s + v.frames_complete, 0);
  const lostRates =
    index.frames_lost === undefined
      ? []
      : trend(rates(points, index.frames_lost), live);
  const jitter = live
    ? at(last, "jitter_p50")
    : mean(busy.map((p) => at(p, "jitter_p50") ?? 0));
  const jitterHigh = live
    ? at(last, "jitter_p90")
    : Math.max(0, ...busy.map((p) => at(p, "jitter_p90") ?? 0));
  const errors = Object.values(viewers?.errors ?? {}).reduce(
    (s, v) => s + v,
    0,
  );
  const ratio = complete + lost ? (lost / (complete + lost)) * 100 : 0;
  const thresholds = series?.thresholds;
  const below = live
    ? (at(last, "below") ?? 0)
    : Math.max(0, ...points.map((p) => at(p, "below") ?? 0));
  const rtts = col("rtt_p50");
  const rtt = live
    ? at(last, "rtt_p50")
    : mean(busy.map((p) => at(p, "rtt_p50") ?? 0).filter((v) => v > 0));
  const rttHigh = live
    ? at(last, "rtt_p90")
    : Math.max(0, ...busy.map((p) => at(p, "rtt_p90") ?? 0));
  const received = series ? receivedBytes(series) : 0;
  const result: Tile[] = [
    {
      id: "viewers",
      label: live ? "Active viewers" : "Viewers",
      value: compact(live ? active : joined, 0),
      detail: live
        ? `${compact(joined, 0)} joined${stale ? ` · ${stale} stale` : ""}`
        : `${compact(active, 0)} active at peak`,
      trend: col("active"),
      tone: stale ? "warning" : undefined,
    },
    ...(thresholds
      ? [
          {
            id: "below",
            label: live ? "Below threshold" : "Below threshold at peak",
            value: compact(below, 0),
            detail: limitsLabel(thresholds),
            trend: col("below"),
            tone: below > 0 ? ("destructive" as const) : undefined,
          },
        ]
      : []),
    {
      id: "bitrate",
      label: live ? "Aggregate bitrate" : "Peak bitrate",
      value: compact(bitrate, 1),
      unit: "Mbps",
      detail: [
        live && active ? `${compact(bitrate / active, 2)} Mbps per viewer` : "",
        received ? `${bytesLabel(received)} received` : "",
      ]
        .filter(Boolean)
        .join(" · "),
      trend: bitrates,
    },
    {
      id: "fps",
      label: live ? "Median FPS" : "Mean FPS",
      value: compact(fps, 1),
      detail:
        fpsLow !== undefined && fpsHigh !== undefined && Number.isFinite(fpsLow)
          ? `p10 ${compact(fpsLow, 1)} · p90 ${compact(fpsHigh, 1)}`
          : undefined,
      trend: col("fps_p50"),
    },
    {
      id: "lost",
      label: "Frames lost",
      value: compact(lost, 0),
      detail: live
        ? `${compact(lostRates.at(-1) ?? 0, 1)} per second`
        : `${compact(ratio, 2)}% of frames`,
      trend: lostRates,
    },
    {
      id: "jitter",
      label: live ? "Frame jitter" : "Mean frame jitter",
      value: compact((jitter ?? 0) / 1000, 1),
      unit: "ms",
      detail:
        jitterHigh !== undefined
          ? `p90 ${compact(jitterHigh / 1000, 1)} ms`
          : undefined,
      trend: col("jitter_p50"),
    },
  ];
  if (rtts.some((v) => v > 0))
    result.push({
      id: "rtt",
      label: live ? "RTT" : "Mean RTT",
      value: compact(rtt ?? 0, 1),
      unit: "ms",
      detail:
        rttHigh !== undefined && rttHigh > 0
          ? `p90 ${compact(rttHigh, 1)} ms`
          : undefined,
      trend: rtts,
    });
  if (errors)
    result.push({
      id: "errors",
      label: "Errors",
      value: compact(errors, 0),
      detail: Object.keys(viewers?.errors ?? {}).join(", "),
      tone: "destructive",
    });
  return result;
}

function limitsLabel(t: NonNullable<Series["thresholds"]>) {
  return [
    t.min_fps ? `fps < ${compact(t.min_fps, 1)}` : "",
    t.min_bitrate_bps
      ? `bitrate < ${compact(t.min_bitrate_bps / 1e6, 2)} Mbps`
      : "",
    t.max_jitter_us ? `jitter > ${compact(t.max_jitter_us / 1000, 1)} ms` : "",
    t.max_rtt_ms ? `rtt > ${compact(t.max_rtt_ms, 0)} ms` : "",
  ]
    .filter(Boolean)
    .join(" · ");
}

export function RunStats({
  series,
  viewers,
  live,
  now,
}: {
  series?: Series;
  viewers?: RunViewers;
  live: boolean;
  now: number;
}) {
  const items = useMemo(
    () => tiles(series, viewers, live, now),
    [series, viewers, live, now],
  );
  return (
    <Stats
      aria-label="Run metrics"
      className={items.length > 6 ? "labui:xl:grid-cols-4" : undefined}
    >
      {items.map((tile) => (
        <Stat key={tile.id}>
          <StatLabel>{tile.label}</StatLabel>
          <div className="labui:flex labui:items-end labui:justify-between labui:gap-3">
            <StatValue
              className={
                tile.tone === "destructive"
                  ? "labui:text-destructive"
                  : tile.tone === "warning"
                    ? "labui:text-warning-foreground"
                    : undefined
              }
            >
              {tile.value}
              {tile.unit && (
                <span className="labui:ml-1 labui:text-sm labui:font-normal labui:text-muted-foreground">
                  {tile.unit}
                </span>
              )}
            </StatValue>
            {tile.trend && tile.trend.length > 1 && (
              <Sparkline
                values={tile.trend}
                label={`${tile.label} trend`}
                width={56}
                height={22}
              />
            )}
          </div>
          {tile.detail && <StatDetail>{tile.detail}</StatDetail>}
        </Stat>
      ))}
    </Stats>
  );
}
