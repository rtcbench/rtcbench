import { useMemo, useState } from "react";
import { FileDownIcon } from "lucide-react";
import {
  Button,
  clockLabel,
  column,
  elapsedLabel,
  fieldIndex,
  hasTimelineData,
  num,
  Table,
  type TableColumn,
  timelineCSV,
  TimelineChart,
  ToggleGroup,
  ToggleGroupItem,
} from "@labpane/ui";
import type { Series } from "./api";
import { metricsFor } from "./metrics";

function seriesColumns(series: Series): TableColumn<number[]>[] {
  const index = fieldIndex(series);
  const at = (p: number[], field: string) => {
    const i = index[field];
    return i === undefined ? 0 : (p[i] ?? 0);
  };
  return [
    {
      ...column(
        "t",
        "Time",
        (p) => at(p, "t"),
        (p) => elapsedLabel(at(p, "t") - series.start),
      ),
      rowHeader: true,
      className: "labui:font-mono labui:tabular-nums",
    },
    num("viewers", "Joined", (p) => at(p, "viewers")),
    num("active", "Active", (p) => at(p, "active")),
    num("bitrate", "Bitrate", (p) => at(p, "bitrate") / 1e6, 1, "Mbps"),
    num("fps_p10", "FPS p10", (p) => at(p, "fps_p10"), 1),
    num("fps_p50", "FPS p50", (p) => at(p, "fps_p50"), 1),
    num("fps_p90", "FPS p90", (p) => at(p, "fps_p90"), 1),
    num("dec", "Decoder FPS", (p) => at(p, "dec_fps_p50"), 1),
    num("jitter_p50", "Jitter p50", (p) => at(p, "jitter_p50") / 1000, 1, "ms"),
    num("jitter_p90", "Jitter p90", (p) => at(p, "jitter_p90") / 1000, 1, "ms"),
    num("rtt_p50", "RTT p50", (p) => at(p, "rtt_p50"), 0, "ms"),
    num("rtt_p90", "RTT p90", (p) => at(p, "rtt_p90"), 0, "ms"),
    num("below", "Below threshold", (p) => at(p, "below")),
    num("s2", "S2", (p) => at(p, "layers_s2")),
    num("s1", "S1", (p) => at(p, "layers_s1")),
    num("s0", "S0", (p) => at(p, "layers_s0")),
    num("lost", "Frames lost", (p) => at(p, "frames_lost")),
    num("pli", "PLI", (p) => at(p, "pli")),
    num("errors", "Errors", (p) => at(p, "errors")),
  ];
}

function downloadCSV(name: string, series: Series) {
  const url = URL.createObjectURL(
    new Blob([timelineCSV(series)], { type: "text/csv" }),
  );
  const link = document.createElement("a");
  link.href = url;
  link.download = `${name.replace(/[\/:]+/g, "-")}-timeline.csv`;
  link.click();
  URL.revokeObjectURL(url);
}

export function TimelineSection({
  name,
  series,
  live,
}: {
  name: string;
  series: Series;
  live: boolean;
}) {
  const [view, setView] = useState("charts");
  const [hover, setHover] = useState<number | null>(null);
  const columns = useMemo(() => seriesColumns(series), [series]);
  const shown = useMemo(
    () =>
      metricsFor(series).filter((metric) => hasTimelineData(series, metric)),
    [series],
  );
  const interval =
    series.interval >= 1000
      ? `${series.interval / 1000} s`
      : `${series.interval} ms`;
  return (
    <section className="labui:flex labui:flex-col labui:gap-4">
      <div className="labui:flex labui:flex-wrap labui:items-center labui:justify-between labui:gap-3">
        <h2 className="labui:text-lg labui:font-semibold">
          Timeline
          <span className="labui:ml-2 labui:text-sm labui:font-normal labui:text-muted-foreground labui:tabular-nums">
            {hover !== null
              ? `at ${elapsedLabel(hover - series.start)} · ${clockLabel(hover)}`
              : `every ${interval}${live ? ", live" : ""}`}
          </span>
        </h2>
        <div className="labui:flex labui:items-center labui:gap-2">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => downloadCSV(name, series)}
            disabled={!series.points.length}
          >
            <FileDownIcon />
            CSV
          </Button>
          <ToggleGroup
            type="single"
            variant="outline"
            size="sm"
            value={view}
            onValueChange={(value) => value && setView(value)}
            aria-label="Timeline view"
          >
            <ToggleGroupItem value="charts">Charts</ToggleGroupItem>
            <ToggleGroupItem value="table">Table</ToggleGroupItem>
          </ToggleGroup>
        </div>
      </div>
      {view === "charts" ? (
        <div className="labui:grid labui:gap-4 labui:md:grid-cols-2 labui:2xl:grid-cols-3">
          {shown.map((metric) => (
            <TimelineChart
              key={metric.id}
              series={series}
              metric={metric}
              live={live}
              hover={hover}
              onHover={setHover}
            />
          ))}
        </div>
      ) : (
        <Table
          label="Timeline"
          empty="No points yet"
          rows={series.points}
          rowKey={(p) => String(p[0])}
          columns={columns}
          rowHeight={32}
        />
      )}
    </section>
  );
}
