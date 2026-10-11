import { densityPresets, useTabSize } from "./context";

type Point = [number, number];
const reach = 16;
const shoulder: Point[] = [
  [0, 43.5],
  [8.56, 43.5],
  [13, 30],
  [16, 21],
  [19, 12],
  [22, 8],
  [30, 8],
];

function corner(gap: number): Point[] {
  if (gap >= reach) return shoulder;
  const wall = 0.5 - gap,
    radius = Math.max(0, gap - 0.5);
  const source: Point[] = [
    [wall, 59],
    [wall, 50.44],
    [wall + radius * 0.4477, 43.5],
    [wall + radius, 43.5],
    [wall + radius * 1.5523, 43.5],
    ...shoulder.slice(2),
  ];
  const target: Point[] = [
    [wall, 59],
    [wall, 47.33],
    [wall, 28.4183],
    [wall, 24],
    [wall, 19.5817],
    [wall + 1.7909, 15.5817],
    [wall + 4.6863, 12.6863],
    [wall + 7.5817, 9.7909],
    [wall + 11.5817, 8],
    [wall + 16, 8],
  ];
  const t = 1 - gap / reach,
    amount = t * t * (3 - 2 * t);
  return source.map(([x, y], i) => [
    x + (target[i]![0] - x) * amount,
    y + (target[i]![1] - y) * amount,
  ]);
}

export function tabShape(
  leftGap = reach,
  rightGap = reach,
  size = densityPresets.comfortable,
) {
  const width = size.width + 16;
  const left = corner(leftGap);
  const right = corner(rightGap)
    .map(([x, y]) => [width - x, y] as Point)
    .reverse();
  const coords = ([x, y]: Point) =>
    [
      x,
      size.height -
        0.5 +
        (y - 43.5) * (y < 43.5 ? (size.contentHeight - 0.5) / 35.5 : 1),
    ]
      .map((v) => Math.round(v * 1000) / 1000)
      .join(" ");
  const curves = (points: Point[]) =>
    points
      .slice(1)
      .map((p, i) => (i % 3 === 0 ? " C" : " ") + coords(p))
      .join("");
  const outline = `M${coords(left[0]!)}${curves(left)} H${right[0]![0]}${curves(right)}`;
  return [
    `${outline} V${size.height + (leftGap < reach || rightGap < reach ? 15 : 0)} H${left[0]![0]} Z`,
    outline,
  ] as const;
}

export function paneShape(
  leftGap: number,
  rightGap: number,
  bottom: number,
  morph = true,
  size = densityPresets.comfortable,
) {
  const width = size.width + 16,
    baseline = size.height - 0.5,
    cornerBottom = size.height + 15;
  const left = 0.5 - leftGap,
    right = width + rightGap - 0.5;
  const rightCorner =
    morph && rightGap < reach
      ? ""
      : ` H${right - 15.5} A15.5 15.5 0 0 1 ${right} ${cornerBottom}`;
  const leftCorner =
    morph && leftGap < reach
      ? ""
      : ` A15.5 15.5 0 0 1 ${left + 15.5} ${baseline} H0`;
  return `${tabShape(morph ? leftGap : reach, morph ? rightGap : reach, size)[1]}${rightCorner} V${bottom - 15.5} A15.5 15.5 0 0 1 ${right - 15.5} ${bottom} H${left + 15.5} A15.5 15.5 0 0 1 ${left} ${bottom - 15.5} V${cornerBottom}${leftCorner} Z`;
}

export function TabSurface({
  selected,
  keyboardFocus = false,
  width,
}: {
  selected: boolean;
  keyboardFocus?: boolean;
  width?: number;
}) {
  const density = useTabSize(),
    size = { ...density, width: width ?? density.width },
    viewWidth = size.width + 16;
  const [fill, outline] = tabShape(undefined, undefined, size);
  return (
    <svg
      aria-hidden="true"
      viewBox={`0 0 ${viewWidth} ${size.height}`}
      strokeWidth={size.borderWidth}
      preserveAspectRatio="none"
      className="lab:pointer-events-none lab:absolute lab:top-0 lab:-left-2 lab:h-full lab:w-[calc(100%+16px)] lab:overflow-visible"
    >
      <path
        d={fill}
        className={
          selected
            ? "lab:fill-card"
            : "lab:fill-[color-mix(in_srgb,var(--lab-card)_45%,var(--lab-background))] lab:transition-colors lab:group-hover:fill-[color-mix(in_srgb,var(--lab-card)_65%,var(--lab-background))]"
        }
      />
      <path
        d={outline}
        data-tab-outline
        className="lab:fill-none lab:stroke-border"
      />
      {keyboardFocus && (
        <path
          d={outline}
          className="lab:hidden lab:fill-none lab:stroke-ring lab:stroke-[3] lab:group-has-[a:focus-visible]:block"
        />
      )}
      {!selected && (
        <path
          d={`M0 ${size.height - 0.5} H${viewWidth}`}
          className="lab:fill-none lab:stroke-border"
        />
      )}
    </svg>
  );
}
