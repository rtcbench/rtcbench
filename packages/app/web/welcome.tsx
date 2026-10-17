import {
  Fragment,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  ChevronDownIcon,
  ChevronRightIcon,
  DownloadIcon,
  FileTextIcon,
  StarIcon,
} from "lucide-react";
import {
  Alert,
  AlertDescription,
  AlertTitle,
  Badge,
  Button,
  cn,
  CopyButton,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
  Label,
  Switch,
} from "@labpane/ui";
import { Logo } from "./brand";
import {
  ChromiumMark,
  DiscordMark,
  GitHubMark,
  JitsiMark,
  LiveKitMark,
  MediasoupMark,
  PdfMark,
} from "./marks";
import paper from "./paper.json";

export const repository = "rtcbench/rtcbench";
export const community =
  "https://github.com/rtcbench/rtcbench/wiki/Discord-link";

export type Platform = "windows" | "macos" | "linux";
export type Variant = "desktop" | "headless";

export type Build = {
  id: string;
  target: string;
  variant: Variant;
  platform: Platform;
  os: string;
  arch: string;
  file: string;
  url: string;
  size?: number;
  published: boolean;
  webkitgtk: boolean;
};

export type Release = {
  tag: string;
  prerelease: boolean;
  assets: { name: string; size: number; url: string }[];
};

const systems: Record<Platform, string> = {
  windows: "Windows",
  macos: "macOS",
  linux: "Linux",
};
const architectures = { amd64: "x64", arm64: "arm64" };

export const files = [
  "rtcbench-linux-arm64-webkitgtk.tar.gz",
  "rtcbench-linux-amd64-webkitgtk.tar.gz",
  "rtcbench-macos-arm64.zip",
  "rtcbench-macos-amd64.zip",
  "rtcbench-windows-arm64.exe",
  "rtcbench-windows-amd64.exe",
  "rtcbench-headless-linux-arm64.tar.gz",
  "rtcbench-headless-linux-amd64.tar.gz",
  "rtcbench-headless-macos-arm64.zip",
  "rtcbench-headless-macos-amd64.zip",
  "rtcbench-headless-windows-arm64.exe",
  "rtcbench-headless-windows-amd64.exe",
];

export function parseBuild(
  file: string,
): Omit<Build, "url" | "size" | "published"> | undefined {
  const match =
    /^rtcbench(-headless)?-(windows|macos|linux)-(amd64|arm64)(-webkitgtk)?\.(exe|zip|tar\.gz)$/.exec(
      file,
    );
  if (!match) return undefined;
  const variant: Variant = match[1] ? "headless" : "desktop";
  const platform = match[2] as Platform;
  const arch = match[3] as keyof typeof architectures;
  return {
    id: `${variant}-${platform}-${arch}`,
    target: `${platform}-${arch}`,
    variant,
    platform,
    os: systems[platform],
    arch:
      platform === "macos"
        ? arch === "arm64"
          ? "Apple silicon"
          : "Intel"
        : architectures[arch],
    file,
    webkitgtk: !!match[4],
  };
}

export function detectPlatform(
  userAgent: string,
  platform = "",
): Platform | undefined {
  const text = `${platform} ${userAgent}`;
  if (/windows/i.test(text)) return "windows";
  if (/iphone|ipad|android/i.test(text)) return undefined;
  if (/mac/i.test(text)) return "macos";
  if (/linux|x11/i.test(text)) return "linux";
  return undefined;
}

export const defaultTarget = (platform: Platform | undefined, arm?: boolean) =>
  platform &&
  `${platform}-${(platform === "macos" ? arm !== false : !!arm) ? "arm64" : "amd64"}`;

export const assetUrl = (repo: string, file: string) =>
  `https://github.com/${repo}/releases/latest/download/${file}`;
export const releasesUrl = (repo: string) =>
  `https://github.com/${repo}/releases`;

export const formatSize = (bytes: number) =>
  `${Math.max(1, Math.round(bytes / 1e6))} MB`;

export function parseReleases(data: unknown): Release[] {
  if (!Array.isArray(data)) return [];
  return data
    .filter((item) => item && !item.draft && typeof item.tag_name === "string")
    .map((item) => ({
      tag: item.tag_name as string,
      prerelease: !!item.prerelease,
      assets: Array.isArray(item.assets)
        ? item.assets.map(
            (asset: {
              name: string;
              size: number;
              browser_download_url: string;
            }) => ({
              name: asset.name,
              size: asset.size,
              url: asset.browser_download_url,
            }),
          )
        : [],
    }));
}

export type Hashes = { sha256?: string; md5?: string };

export function parseChecksums(text: string): Record<string, Hashes> {
  const sums: Record<string, Hashes> = {};
  for (const line of text.split("\n")) {
    const match = /^([0-9a-f]{32}|[0-9a-f]{64})\s+\*?(\S+)$/i.exec(line.trim());
    if (!match) continue;
    const hash = match[1]!.toLowerCase();
    const entry = sums[match[2]!] ?? (sums[match[2]!] = {});
    entry[hash.length === 64 ? "sha256" : "md5"] = hash;
  }
  return sums;
}

export type Version = {
  major: number;
  minor: number;
  patch: number;
  pre?: string;
};

export function parseVersion(tag: string): Version | undefined {
  const match = /^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?/.exec(tag);
  if (!match) return undefined;
  return {
    major: Number(match[1]),
    minor: Number(match[2]),
    patch: Number(match[3]),
    pre: match[4],
  };
}

export function compareVersions(a: string, b: string) {
  const x = parseVersion(a);
  const y = parseVersion(b);
  if (!x || !y) return x ? -1 : y ? 1 : b.localeCompare(a);
  return (
    y.major - x.major ||
    y.minor - x.minor ||
    y.patch - x.patch ||
    (x.pre ? 1 : 0) - (y.pre ? 1 : 0) ||
    (y.pre ?? "").localeCompare(x.pre ?? "", undefined, { numeric: true })
  );
}

export function headlessOnly(tag: string) {
  const version = parseVersion(tag);
  return !!version && version.major === 0 && version.minor < 17;
}

export type VersionGroup = { minor: string; top: Release; others: Release[] };

export function groupReleases(releases: Release[]): VersionGroup[] {
  const sorted = [...releases].sort((a, b) => compareVersions(a.tag, b.tag));
  const groups: VersionGroup[] = [];
  for (const release of sorted) {
    const version = parseVersion(release.tag);
    const minor = version ? `${version.major}.${version.minor}` : release.tag;
    const group = groups.find((item) => item.minor === minor);
    if (group) group.others.push(release);
    else groups.push({ minor, top: release, others: [] });
  }
  return groups;
}

export function curlCommand(build: Pick<Build, "file" | "url">) {
  if (build.file.endsWith(".tar.gz")) return `curl -fsSL ${build.url} | tar xz`;
  if (build.file.endsWith(".zip"))
    return `curl -fsSLo rtcbench.zip ${build.url} && unzip -o rtcbench.zip`;
  return `curl.exe -fsSLo rtcbench.exe ${build.url}`;
}

export function buildsOf(repo: string, release?: Release): Build[] {
  return files.map((file) => {
    const asset = release?.assets.find((item) => item.name === file);
    return {
      ...parseBuild(file)!,
      url: asset
        ? asset.url
        : release
          ? `https://github.com/${repo}/releases/tag/${release.tag}`
          : assetUrl(repo, file),
      size: asset?.size,
      published: !release || !!asset,
    };
  });
}

export const bibtex = `@inproceedings{ram2026rtcbench,
  title = {RTCBench: A Scalable Benchmarking Framework for Multi-Party Video Conferencing},
  author = {Ram, Evan and Tran, Tuan and Son, Youngwook and Ha, Sangtae},
  booktitle = {${paper.proceedings}},
  publisher = {ACM},
  address = {Rio de Janeiro, Brazil},
  year = {2026},
  doi = {${paper.doi}}
}`;

const platforms = [
  { name: "Jitsi", url: "https://jitsi.org", Mark: JitsiMark },
  { name: "LiveKit", url: "https://livekit.io", Mark: LiveKitMark },
  { name: "mediasoup", url: "https://mediasoup.org", Mark: MediasoupMark },
];

const cores = [4, 8, 12, 16];
const scaling = {
  rtcbench: [441, 701, 1121, 2512],
  chromium: [8, 15, 21, 29],
};

const number = (value: number) => value.toLocaleString("en-US");

type NavigatorData = Navigator & {
  userAgentData?: {
    platform: string;
    getHighEntropyValues?: (
      hints: string[],
    ) => Promise<{ architecture?: string }>;
  };
};

function currentPlatform() {
  if (typeof navigator === "undefined") return undefined;
  const { userAgent, userAgentData } = navigator as NavigatorData;
  return detectPlatform(userAgent, userAgentData?.platform);
}

type SiteData = { releases: Release[] };

function useSiteData(): SiteData {
  const [data, setData] = useState<SiteData>({ releases: [] });
  useEffect(() => {
    let cancelled = false;
    const read = (file: string) =>
      fetch(new URL(file, document.baseURI))
        .then((res) => (res.ok ? res.json() : null))
        .catch(() => null);
    read("releases.json").then((json) => {
      if (!cancelled && json)
        setData((data) => ({ ...data, releases: parseReleases(json) }));
    });
    return () => {
      cancelled = true;
    };
  }, []);
  return data;
}

export function Welcome({
  releases,
  attention = 0,
  visit,
}: {
  releases?: string;
  attention?: number;
  visit: (path: string) => void;
}) {
  return (
    <div className="labui:mx-auto labui:flex labui:w-full labui:max-w-3xl labui:flex-col labui:gap-14 labui:px-4 labui:py-10">
      <header className="labui:relative labui:flex labui:flex-col labui:gap-6">
        <div className="labui:flex labui:items-start labui:justify-between labui:gap-4">
          <div className="labui:flex labui:items-center labui:gap-3">
            <Logo className="labui:size-10" />
            <h1 className="labui:font-display labui:text-3xl labui:font-semibold labui:tracking-tight">
              RTCBench
            </h1>
          </div>
          {releases && <Repository repo={releases} />}
        </div>
        <div className="labui:flex labui:w-fit labui:max-w-2xl labui:flex-col labui:gap-6">
          <p className="labui:text-lg labui:text-muted-foreground">
            Benchmark SFUs with thousands of video streams from one machine
          </p>
          <WorksWith />
        </div>
        {releases ? (
          <Download repo={releases} attention={attention} />
        ) : (
          <div className="labui:flex labui:flex-wrap labui:gap-2">
            <Button size="lg" onClick={() => visit("runs")}>
              Runs
            </Button>
            <Button
              size="lg"
              variant="outline"
              onClick={() => visit("configs")}
            >
              Configs
            </Button>
            <Button size="lg" variant="ghost" asChild>
              <a
                href={`https://github.com/${repository}`}
                target="_blank"
                rel="noreferrer"
              >
                <StarIcon aria-hidden="true" />
                GitHub
              </a>
            </Button>
          </div>
        )}
      </header>

      <div className="labui:flex labui:flex-col labui:gap-8">
        <Paper />
        <figure className="labui:flex labui:gap-1 labui:lg:-ml-[78px]">
          <span
            aria-hidden="true"
            className="labui:self-center labui:rotate-180 labui:text-xs labui:text-muted-foreground labui:[writing-mode:vertical-rl]"
          >
            # of full-quality 1080p25 streams
          </span>
          <div className="labui:flex labui:min-w-0 labui:flex-1 labui:flex-col labui:gap-3">
            <Scaling />
            <figcaption
              className="labui:text-center labui:text-sm labui:font-semibold"
              style={{
                paddingRight: `${((chart.right - chart.left) / chart.width) * 100}%`,
              }}
            >
              Streams per host (LiveKit, CloudLab c8220, two 10-core Xeon
              E5-2660 v2)
            </figcaption>
          </div>
        </figure>
      </div>
    </div>
  );
}

function WorksWith() {
  return (
    <div className="labui:mb-6 labui:flex labui:flex-wrap labui:items-center labui:justify-between labui:gap-x-6 labui:gap-y-3">
      <span className="labui:text-sm labui:text-muted-foreground">
        Works with
      </span>
      {platforms.map(({ name, url, Mark }) => (
        <a
          key={name}
          href={url}
          target="_blank"
          rel="noreferrer"
          className="labui:inline-flex labui:items-center labui:gap-2 labui:font-display labui:text-xl labui:font-semibold labui:tracking-tight labui:text-foreground/80 labui:hover:text-foreground"
        >
          <Mark className="labui:size-7" />
          {name}
        </a>
      ))}
      <span className="labui:text-sm labui:text-muted-foreground">
        … and more!
      </span>
    </div>
  );
}

export function Download({
  repo,
  attention = 0,
}: {
  repo: string;
  attention?: number;
}) {
  const [platform] = useState(currentPlatform);
  const [arm, setArm] = useState<boolean | undefined>(() =>
    typeof navigator !== "undefined" &&
    /aarch64|arm64/i.test(navigator.userAgent)
      ? true
      : undefined,
  );
  useEffect(() => {
    (navigator as NavigatorData).userAgentData
      ?.getHighEntropyValues?.(["architecture"])
      .then((values) => {
        if (values.architecture) setArm(values.architecture === "arm");
      })
      .catch(() => {});
  }, []);
  const { releases } = useSiteData();
  const groups = useMemo(() => groupReleases(releases), [releases]);
  const [tag, setTag] = useState<string>();
  const release = releases.find((item) => item.tag === tag) ?? groups[0]?.top;
  const [headless, setHeadless] = useState(platform === "linux");
  const [target, setTarget] = useState<string>();
  const [started, setStarted] = useState(false);
  const [curl, setCurl] = useState(false);
  const [sums, setSums] = useState<
    Record<string, Record<string, Hashes> | null>
  >({});
  const requested = useRef(new Set<string>());
  const [glow, setGlow] = useState(false);
  const anchor = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!attention) return;
    anchor.current?.scrollIntoView({ behavior: "smooth", block: "center" });
    setGlow(true);
    const timer = setTimeout(() => setGlow(false), 800);
    return () => clearTimeout(timer);
  }, [attention]);
  const switchId = useId();
  const legacy = !!release && headlessOnly(release.tag);
  const current = release?.tag;
  useEffect(() => {
    if (!curl || !current || requested.current.has(current)) return;
    requested.current.add(current);
    fetch(new URL(`checksums/${current}.txt`, document.baseURI))
      .then((res) => (res.ok ? res.text() : null))
      .catch(() => null)
      .then((text) =>
        setSums((all) => ({
          ...all,
          [current]: text === null ? null : parseChecksums(text),
        })),
      );
  }, [curl, current]);
  const variant: Variant = headless || legacy ? "headless" : "desktop";
  const builds = buildsOf(repo, release).filter(
    (build) => build.variant === variant,
  );
  const targets = files
    .map((file) => parseBuild(file)!)
    .filter((option) => option.variant === variant);
  const wanted = target ?? defaultTarget(platform, arm);
  const build =
    builds.find((item) => item.target === wanted) ??
    (platform ? builds[0] : undefined);
  const github = `https://github.com/${repo}`;
  return (
    <div className="labui:flex labui:flex-col labui:gap-3">
      <div className="labui:flex labui:flex-wrap labui:items-stretch labui:gap-2">
        <div
          ref={anchor}
          className={cn("labui:flex labui:rounded-lg", glow && "rtc-attention")}
        >
          <Button
            size="lg"
            asChild
            className="labui:h-auto labui:min-w-80 labui:flex-1 labui:justify-start labui:rounded-r-none labui:px-6 labui:text-base"
          >
            <a
              href={build?.url ?? releasesUrl(repo)}
              target="_blank"
              rel="noreferrer"
              onClick={() => setStarted(true)}
            >
              <DownloadIcon aria-hidden="true" />
              <span className="labui:flex labui:flex-col labui:items-start labui:leading-tight">
                {build ? `Download for ${build.os} ${build.arch}` : "Download"}
                {build && (
                  <span className="labui:flex labui:gap-3 labui:text-xs labui:leading-tight labui:opacity-90">
                    <span>
                      <span className="labui:font-semibold">
                        {release?.tag ?? "latest"}
                      </span>
                      {build.variant === "headless" && (
                        <span className="labui:font-normal labui:italic">
                          {" "}
                          Headless
                        </span>
                      )}
                    </span>
                    {build.size !== undefined ? (
                      <span className="labui:font-normal">
                        {formatSize(build.size)}
                      </span>
                    ) : (
                      !build.published && (
                        <span className="labui:font-normal labui:italic">
                          source only
                        </span>
                      )
                    )}
                  </span>
                )}
              </span>
            </a>
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                size="icon-lg"
                aria-label="Choose a platform"
                className="labui:h-auto labui:rounded-l-none labui:border-l labui:border-primary-foreground/25"
              >
                <ChevronDownIcon aria-hidden="true" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuRadioGroup
                value={build?.target ?? ""}
                onValueChange={(value) => {
                  setTarget(value);
                  setHeadless(value.startsWith("linux"));
                }}
              >
                {targets.map((option) => {
                  const item = builds.find((build) => build.id === option.id);
                  return (
                    <DropdownMenuRadioItem
                      key={option.id}
                      value={option.target}
                    >
                      {option.os} {option.arch}
                      <span className="labui:ml-auto labui:pl-4 labui:text-xs labui:text-muted-foreground">
                        {item?.size !== undefined
                          ? formatSize(item.size)
                          : item && !item.published
                            ? "source only"
                            : ""}
                      </span>
                    </DropdownMenuRadioItem>
                  );
                })}
              </DropdownMenuRadioGroup>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
        <div className="labui:flex labui:flex-col labui:justify-between labui:gap-1">
          {release && (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline" className="labui:min-w-28">
                  {release.tag}
                  <ChevronDownIcon
                    aria-hidden="true"
                    className="labui:ml-auto"
                  />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start">
                <DropdownMenuRadioGroup
                  value={release.tag}
                  onValueChange={setTag}
                >
                  {groups.map((group) =>
                    group.others.length === 0 ? (
                      <DropdownMenuRadioItem
                        key={group.minor}
                        value={group.top.tag}
                      >
                        {group.top.tag}
                      </DropdownMenuRadioItem>
                    ) : (
                      <DropdownMenuSub key={group.minor}>
                        <DropdownMenuSubTrigger className="labui:pl-8">
                          {group.top.tag}
                        </DropdownMenuSubTrigger>
                        <DropdownMenuSubContent>
                          <DropdownMenuRadioGroup
                            value={release.tag}
                            onValueChange={setTag}
                          >
                            {[group.top, ...group.others].map((item) => (
                              <DropdownMenuRadioItem
                                key={item.tag}
                                value={item.tag}
                              >
                                {item.tag}
                              </DropdownMenuRadioItem>
                            ))}
                          </DropdownMenuRadioGroup>
                        </DropdownMenuSubContent>
                      </DropdownMenuSub>
                    ),
                  )}
                </DropdownMenuRadioGroup>
              </DropdownMenuContent>
            </DropdownMenu>
          )}
          {build && (
            <div className="labui:flex labui:items-center labui:gap-2 labui:px-1">
              <Switch
                id={switchId}
                checked={headless || legacy}
                disabled={legacy}
                onCheckedChange={setHeadless}
              />
              <Label
                htmlFor={switchId}
                className={cn(legacy && "labui:text-muted-foreground")}
              >
                Headless
              </Label>
            </div>
          )}
        </div>
      </div>
      {build?.webkitgtk && (
        <div className="labui:flex labui:flex-col labui:gap-2 labui:text-sm labui:text-muted-foreground">
          <p>Needs GTK 3 and WebKitGTK 4.1 unless you go headless.</p>
          <div className="labui:flex labui:flex-col labui:gap-1">
            <span>Debian and Ubuntu</span>
            <Code>sudo apt install libgtk-3-0 libwebkit2gtk-4.1-0</Code>
          </div>
          <div className="labui:flex labui:flex-col labui:gap-1">
            <span>Fedora</span>
            <Code>sudo dnf install gtk3 webkit2gtk4.1</Code>
          </div>
        </div>
      )}
      {build && (
        <div className="labui:flex labui:flex-col labui:gap-2">
          <button
            type="button"
            aria-expanded={curl}
            className="labui:inline-flex labui:w-fit labui:items-center labui:gap-1 labui:text-sm labui:text-muted-foreground labui:hover:text-foreground"
            onClick={() => setCurl((open) => !open)}
          >
            <ChevronRightIcon
              aria-hidden="true"
              className={cn(
                "labui:size-4 labui:transition-transform",
                curl && "labui:rotate-90",
              )}
            />
            {curl
              ? "Hide download command and hash"
              : "Show download command and hash"}
          </button>
          {curl && (
            <CodeBlock value={curlCommand(build)} label="Copy curl command" />
          )}
          {curl && current && sums[current] !== undefined && (
            <HashList hashes={sums[current]?.[build.file]} />
          )}
        </div>
      )}
      {started && (
        <Alert>
          <StarIcon aria-hidden="true" />
          <AlertTitle>Download started</AlertTitle>
          <AlertDescription>
            If RTCBench proves useful, a star on GitHub helps others find it.
            <Button variant="outline" size="sm" asChild>
              <a href={github} target="_blank" rel="noreferrer">
                Star on GitHub
              </a>
            </Button>
          </AlertDescription>
        </Alert>
      )}
    </div>
  );
}

export function DownloadButton({ onClick }: { onClick: () => void }) {
  return (
    <Badge asChild>
      <button
        type="button"
        className="labui:cursor-pointer labui:hover:bg-primary/90"
        onClick={onClick}
      >
        <DownloadIcon aria-hidden="true" className="labui:size-3" />
        Download
      </button>
    </Badge>
  );
}

const chart = { width: 716, height: 320, left: 52, right: 86 };

function Scaling() {
  const { width, height, left, right } = chart;
  const top = 40;
  const bottom = 40;
  const mark = 25;
  const plot = { w: width - left - right, h: height - top - bottom };
  const floor = Math.log10(5);
  const ceiling = Math.log10(4000);
  const minor = [1, 10, 100, 1000]
    .flatMap((decade) => [2, 3, 4, 5, 6, 7, 8, 9].map((n) => n * decade))
    .filter((value) => value > 5 && value < 4000);
  const last = cores.length - 1;
  const x = (index: number) => left + (plot.w * index) / last;
  const y = (value: number) =>
    top + plot.h * (1 - (Math.log10(value) - floor) / (ceiling - floor));
  const point = (value: number, index: number) => `${x(index)} ${y(value)}`;
  const line = (values: number[]) =>
    values
      .map((value, index) => `${index ? "L" : "M"}${point(value, index)}`)
      .join(" ");
  const ratio = Math.floor(scaling.rtcbench[last]! / scaling.chromium[last]!);
  const brace = (bx: number, y1: number, y2: number, w = 7) => {
    const mid = (y1 + y2) / 2;
    return `M${bx} ${y1} Q${bx + w} ${y1} ${bx + w} ${y1 + w} L${bx + w} ${mid - w} Q${bx + w} ${mid} ${bx + 2 * w} ${mid} Q${bx + w} ${mid} ${bx + w} ${mid + w} L${bx + w} ${y2 - w} Q${bx + w} ${y2} ${bx} ${y2}`;
  };
  const title = (name: string, index: number, value: number) =>
    `${name}, ${cores[index]} cores: ${number(value)} receivers`;
  return (
    <svg
      viewBox={`0 0 ${width} ${height}`}
      role="img"
      aria-label={`LiveKit receivers per host against CPU cores, log scale: RTCBench reaches ${number(scaling.rtcbench[last]!)} at ${cores[last]} cores, Chromium ${scaling.chromium[last]}, ${ratio} times more`}
      className="labui:w-full labui:text-[13px]"
    >
      {minor.map((tick) => (
        <line
          key={tick}
          x1={left}
          x2={left + plot.w}
          y1={y(tick)}
          y2={y(tick)}
          className="labui:stroke-border/50"
        />
      ))}
      {[10, 100, 1000].map((tick) => (
        <g key={tick}>
          <line
            x1={left}
            x2={left + plot.w}
            y1={y(tick)}
            y2={y(tick)}
            className="labui:stroke-border"
          />
          <text
            x={left - 10}
            y={y(tick) + 4}
            textAnchor="end"
            className="labui:fill-muted-foreground"
          >
            {number(tick)}
          </text>
        </g>
      ))}
      {cores.map((core, index) => (
        <text
          key={core}
          x={x(index)}
          y={height - bottom + 20}
          textAnchor="middle"
          className="labui:fill-muted-foreground"
        >
          {core}
        </text>
      ))}
      <text
        x={left + plot.w / 2}
        y={height - 4}
        textAnchor="middle"
        className="labui:fill-muted-foreground"
      >
        CPU cores
      </text>
      <path
        d={line(scaling.chromium)}
        fill="none"
        strokeWidth={2.5}
        strokeDasharray="6 5"
        className="labui:stroke-muted-foreground/70"
      />
      <path
        d={line(scaling.rtcbench)}
        fill="none"
        strokeWidth={3.5}
        strokeLinejoin="round"
        strokeLinecap="round"
        className="labui:stroke-primary"
      />
      {scaling.chromium.map((value, index) => (
        <g key={index}>
          <title>{title("Chromium", index, value)}</title>
          <circle
            cx={x(index)}
            cy={y(value)}
            r={5}
            className="labui:fill-muted-foreground labui:stroke-background"
            strokeWidth={2}
          />
          {index < last && (
            <text
              x={x(index)}
              y={y(value) + 20}
              textAnchor="middle"
              className="labui:fill-muted-foreground labui:font-mono"
            >
              {number(value)}
            </text>
          )}
        </g>
      ))}
      {scaling.rtcbench.map((value, index) => (
        <g key={index}>
          <title>{title("RTCBench", index, value)}</title>
          <circle
            cx={x(index)}
            cy={y(value)}
            r={5}
            className="labui:fill-primary labui:stroke-background"
            strokeWidth={2}
          />
          {index < last && (
            <text
              x={x(index)}
              y={y(value) - 12}
              textAnchor="middle"
              className="labui:fill-foreground labui:font-mono labui:font-semibold"
            >
              {number(value)}
            </text>
          )}
        </g>
      ))}
      <Logo
        x={x(last) - 70}
        y={y(scaling.rtcbench[last]!) - 52}
        width={mark}
        height={mark}
      />
      <text
        x={x(last) - 70 + mark / 2}
        y={y(scaling.rtcbench[last]!) - 17}
        textAnchor="middle"
        className="labui:fill-muted-foreground labui:text-[10px]"
      >
        RTCBench
      </text>
      <text
        x={x(last) - 39}
        y={y(scaling.rtcbench[last]!) - 35}
        className="labui:fill-primary labui:font-semibold"
      >
        {number(scaling.rtcbench[last]!)}
      </text>
      <ChromiumMark
        x={x(last) - 62}
        y={y(scaling.chromium[last]!) + 10}
        width={mark}
        height={mark}
      />
      <text
        x={x(last) - 62 + mark / 2}
        y={y(scaling.chromium[last]!) + 46}
        textAnchor="middle"
        className="labui:fill-muted-foreground labui:text-[10px]"
      >
        Chromium
      </text>
      <text
        x={x(last) - 31}
        y={y(scaling.chromium[last]!) + 27}
        className="labui:fill-muted-foreground labui:font-semibold"
      >
        {scaling.chromium[last]}
      </text>
      <path
        d={brace(
          x(last) + 12,
          y(scaling.rtcbench[last]!),
          y(scaling.chromium[last]!),
        )}
        fill="none"
        strokeWidth={1.5}
        className="labui:stroke-muted-foreground"
      />
      <text
        x={x(last) + 56}
        y={(y(scaling.rtcbench[last]!) + y(scaling.chromium[last]!)) / 2 + 3}
        textAnchor="middle"
        className="labui:fill-foreground labui:font-display labui:text-2xl labui:font-semibold"
      >
        {ratio}×
      </text>
      <text
        x={x(last) + 56}
        y={(y(scaling.rtcbench[last]!) + y(scaling.chromium[last]!)) / 2 + 20}
        textAnchor="middle"
        className="labui:fill-muted-foreground"
      >
        <tspan x={x(last) + 56}>more</tspan>
        <tspan x={x(last) + 56} dy="1.15em">
          streams
        </tspan>
      </text>
    </svg>
  );
}

const link =
  "labui:text-link labui:underline labui:decoration-1 labui:underline-offset-4 labui:hover:decoration-2";

function Repository({ repo }: { repo: string }) {
  return (
    <div className="labui:flex labui:shrink-0 labui:flex-col labui:gap-2 labui:md:absolute labui:md:top-0 labui:md:right-0">
      <Button variant="outline" asChild className="labui:justify-start">
        <a href={`https://github.com/${repo}`} target="_blank" rel="noreferrer">
          <GitHubMark className="labui:size-4" />
          Star on GitHub
        </a>
      </Button>
      <Button
        asChild
        className="labui:justify-start labui:bg-[#5865f2] labui:text-white labui:hover:bg-[#4752c4]"
      >
        <a href={community} target="_blank" rel="noreferrer">
          <DiscordMark className="labui:size-4" />
          Come say hi!
        </a>
      </Button>
    </div>
  );
}

function Paper() {
  const [cite, setCite] = useState(false);
  const citeId = useId();
  const toggle = () => setCite((state) => !state);
  return (
    <section className="labui:flex labui:flex-col labui:gap-3">
      <div
        onClick={(event) => {
          if ((event.target as HTMLElement).closest("a, button")) return;
          toggle();
        }}
        className="labui:-my-2 labui:-ml-3 labui:flex labui:cursor-pointer labui:flex-col labui:gap-3 labui:rounded-lg labui:px-3 labui:py-2 labui:transition-colors labui:hover:bg-muted/60 labui:lg:-ml-14 labui:lg:pl-14"
      >
        <h2 className="rtc-serif labui:text-xl labui:font-semibold labui:lg:-ml-11">
          <button
            type="button"
            aria-expanded={cite}
            aria-controls={citeId}
            onClick={toggle}
            className="labui:flex labui:items-start labui:gap-3 labui:rounded-md labui:text-left labui:outline-none labui:focus-visible:ring-2 labui:focus-visible:ring-ring"
          >
            <PdfMark className="labui:-mt-0.5 labui:size-8 labui:shrink-0" />
            <span className="labui:text-balance">{paper.title}</span>
          </button>
        </h2>
        <div className="labui:flex labui:items-end labui:justify-between labui:gap-4">
          <div className="labui:flex labui:flex-col labui:gap-1 labui:pl-11 labui:text-sm labui:text-muted-foreground labui:lg:pl-0">
            <p>
              {paper.authors.map((author, index) => (
                <Fragment key={author.name}>
                  {index > 0 &&
                    (index === paper.authors.length - 1 ? " and " : ", ")}
                  <a
                    href={author.url}
                    target="_blank"
                    rel="noreferrer"
                    className={link}
                  >
                    {author.name}
                  </a>
                </Fragment>
              ))}
              , {paper.affiliation}
            </p>
            <p>
              <a
                href={paper.venueUrl}
                target="_blank"
                rel="noreferrer"
                className={link}
              >
                {paper.venue}
              </a>
              , {paper.city}. CC BY 4.0.
            </p>
          </div>
          <ChevronDownIcon
            aria-hidden="true"
            className={cn(
              "labui:size-5 labui:shrink-0 labui:text-muted-foreground labui:transition-transform",
              cite && "labui:rotate-180",
            )}
          />
        </div>
      </div>
      {cite && (
        <div
          id={citeId}
          className="labui:ml-11 labui:flex labui:flex-col labui:gap-4 labui:lg:ml-0"
        >
          <div className="labui:flex labui:flex-wrap labui:items-center labui:justify-between labui:gap-x-4 labui:gap-y-2">
            <p className="labui:text-sm">
              Please cite RTCBench using the following (BibTeX)
            </p>
            <Button
              asChild
              className="labui:bg-[#d32f2f] labui:text-white labui:hover:bg-[#b71c1c]"
            >
              <a href={paper.pdf} target="_blank" rel="noreferrer">
                <FileTextIcon aria-hidden="true" />
                Read the paper
              </a>
            </Button>
          </div>
          <CodeBlock value={bibtex} label="Copy BibTeX" wrap={false} />
        </div>
      )}
    </section>
  );
}
function CodeBlock({
  value,
  label,
  className,
  wrap = true,
}: {
  value: string;
  label: string;
  className?: string;
  wrap?: boolean;
}) {
  return (
    <div className={cn("labui:relative", className)}>
      <pre
        className={cn(
          "rtc-code-block labui:pr-10",
          !wrap &&
            "labui:overflow-x-auto labui:break-normal labui:whitespace-pre",
        )}
      >
        {value}
      </pre>
      <CopyButton
        value={value}
        label={label}
        className="labui:absolute labui:top-2 labui:right-2"
      />
    </div>
  );
}

function HashList({ hashes }: { hashes?: Hashes }) {
  if (!hashes) {
    return (
      <p className="labui:text-sm labui:text-muted-foreground labui:italic">
        Hashes not published
      </p>
    );
  }
  return (
    <div className="labui:flex labui:flex-col labui:gap-2 labui:text-sm labui:text-muted-foreground">
      {hashes.sha256 && (
        <div className="labui:flex labui:flex-col labui:gap-1">
          <span>SHA-256</span>
          <CodeBlock value={hashes.sha256} label="Copy SHA-256" />
        </div>
      )}
      {hashes.md5 && (
        <div className="labui:flex labui:flex-col labui:gap-1">
          <span>MD5</span>
          <CodeBlock value={hashes.md5} label="Copy MD5" />
        </div>
      )}
    </div>
  );
}

function Code({ children }: { children: ReactNode }) {
  return (
    <code className="labui:block labui:w-fit labui:rounded-md labui:bg-muted labui:px-2 labui:py-1 labui:font-mono labui:text-xs labui:whitespace-pre-wrap labui:text-foreground">
      {children}
    </code>
  );
}
