import { expect, it } from "bun:test";
import type { Series } from "./api";
import { metrics, metricsFor, receivedBytes } from "./metrics";

const series: Series = {
  start: 0,
  interval: 1000,
  fields: ["t", "active", "bitrate", "fps_p50", "jitter_p50", "below"],
  points: [
    [1000, 1, 1_000_000, 25, 8000, 0],
    [2000, 2, 2_000_000, 24, 9000, 0],
    [4000, 2, 3_000_000, 24, 9000, 1],
  ],
};

it("resolves the run's limits into chart thresholds in the chart's unit", () => {
  const plain = metricsFor(series);
  expect(plain.map((m) => m.id)).toEqual(metrics.map((m) => m.id));
  expect(plain.every((m) => m.threshold === undefined)).toBe(true);
  expect(plain.find((m) => m.id === "viewers")?.status).toBeUndefined();
  expect(plain.some((m) => "limit" in m)).toBe(false);
  const limited = metricsFor({
    ...series,
    thresholds: { min_fps: 20, max_jitter_us: 30_000 },
  });
  expect(limited.find((m) => m.id === "fps")?.threshold).toBe(20);
  expect(limited.find((m) => m.id === "jitter")?.threshold).toBe(30);
  expect(limited.find((m) => m.id === "rtt")?.threshold).toBeUndefined();
  expect(limited.find((m) => m.id === "viewers")?.status).toBe("below");
});

it("integrates the aggregate bitrate into data received", () => {
  expect(receivedBytes(series)).toBe(1_000_000);
  expect(receivedBytes({ ...series, fields: ["t", "active"] })).toBe(0);
  expect(receivedBytes({ ...series, points: [] })).toBe(0);
});
