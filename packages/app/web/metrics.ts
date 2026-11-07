import { fieldIndex, type TimelineMetric } from "@labpane/ui";
import type { Series, Thresholds } from "./api";

export type RunMetric = TimelineMetric & { limit?: keyof Thresholds };

export const metrics: RunMetric[] = [
  {
    id: "viewers",
    title: "Viewers",
    unit: "",
    line: "active",
    lineLabel: "active",
    context: "viewers",
    contextLabel: "joined",
    status: "below",
    statusLabel: "below threshold",
    digits: 0,
    summary: "peak",
  },
  {
    id: "bitrate",
    title: "Bitrate",
    unit: "Mbps",
    line: "bitrate",
    scale: 1e-6,
    digits: 1,
    summary: "peak",
  },
  {
    id: "fps",
    title: "FPS",
    unit: "fps",
    line: "fps_p50",
    lineLabel: "median",
    band: ["fps_p10", "fps_p90"],
    limit: "min_fps",
    thresholdLabel: "min",
    digits: 1,
    summary: "mean",
  },
  {
    id: "lost",
    title: "Frames lost",
    unit: "/s",
    line: "frames_lost",
    rate: true,
    digits: 1,
    summary: "mean",
  },
  {
    id: "jitter",
    title: "Frame jitter",
    unit: "ms",
    line: "jitter_p50",
    lineLabel: "median",
    band: ["jitter_p50", "jitter_p90"],
    bandLabel: "p50–p90",
    limit: "max_jitter_us",
    thresholdLabel: "max",
    scale: 1e-3,
    digits: 1,
    summary: "mean",
  },
  {
    id: "rtt",
    title: "RTT",
    unit: "ms",
    line: "rtt_p50",
    lineLabel: "median",
    band: ["rtt_p50", "rtt_p90"],
    bandLabel: "p50–p90",
    limit: "max_rtt_ms",
    thresholdLabel: "max",
    digits: 1,
    summary: "mean",
    optional: true,
  },
  {
    id: "layers",
    title: "Spatial layers",
    unit: "",
    line: "layers_s2",
    stack: [
      { field: "layers_s0", label: "S0" },
      { field: "layers_s1", label: "S1" },
      { field: "layers_s2", label: "S2" },
    ],
    digits: 0,
    optional: true,
  },
  {
    id: "pli",
    title: "PLI",
    unit: "/s",
    line: "pli",
    rate: true,
    digits: 2,
    summary: "mean",
  },
];

export function metricsFor(series: Series): TimelineMetric[] {
  return metrics.map(({ limit, ...metric }) => {
    const threshold = limit ? series.thresholds?.[limit] : undefined;
    return {
      ...metric,
      status: series.thresholds ? metric.status : undefined,
      threshold:
        threshold === undefined ? undefined : threshold * (metric.scale ?? 1),
    };
  });
}

export function receivedBytes(series: Series) {
  const index = fieldIndex(series);
  const t = index.t ?? 0;
  const bitrate = index.bitrate;
  if (bitrate === undefined) return 0;
  let bytes = 0;
  for (let i = 1; i < series.points.length; i++) {
    const seconds = (series.points[i]![t]! - series.points[i - 1]![t]!) / 1000;
    bytes += ((series.points[i]![bitrate] ?? 0) * seconds) / 8;
  }
  return bytes;
}
