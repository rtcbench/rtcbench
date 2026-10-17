import { expect, it } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import {
  assetUrl,
  bibtex,
  buildsOf,
  compareVersions,
  curlCommand,
  defaultTarget,
  detectPlatform,
  DownloadButton,
  files,
  formatSize,
  groupReleases,
  headlessOnly,
  parseBuild,
  parseChecksums,
  parseReleases,
  parseVersion,
  Welcome,
} from "./welcome";

it("picks the download for the visitor's platform", () => {
  expect(detectPlatform("Mozilla/5.0 (Windows NT 10.0; Win64; x64)")).toBe(
    "windows",
  );
  expect(
    detectPlatform("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)"),
  ).toBe("macos");
  expect(detectPlatform("Mozilla/5.0 (X11; Linux x86_64)")).toBe("linux");
  expect(detectPlatform("Mozilla/5.0 (Linux; Android 14; Pixel 8)")).toBe(
    undefined,
  );
  expect(
    detectPlatform("Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)"),
  ).toBe(undefined);
  expect(detectPlatform("", "Windows")).toBe("windows");
  expect(defaultTarget("linux", true)).toBe("linux-arm64");
  expect(defaultTarget("windows")).toBe("windows-amd64");
  expect(defaultTarget("windows", true)).toBe("windows-arm64");
  expect(defaultTarget("macos")).toBe("macos-arm64");
  expect(defaultTarget("macos", true)).toBe("macos-arm64");
  expect(defaultTarget("macos", false)).toBe("macos-amd64");
  expect(defaultTarget("linux", false)).toBe("linux-amd64");
  expect(defaultTarget(undefined)).toBe(undefined);
});

it("recognises release assets by name", () => {
  expect(files.map((file) => parseBuild(file)?.id)).toEqual([
    "desktop-linux-arm64",
    "desktop-linux-amd64",
    "desktop-macos-arm64",
    "desktop-macos-amd64",
    "desktop-windows-arm64",
    "desktop-windows-amd64",
    "headless-linux-arm64",
    "headless-linux-amd64",
    "headless-macos-arm64",
    "headless-macos-amd64",
    "headless-windows-arm64",
    "headless-windows-amd64",
  ]);
  expect(parseBuild("rtcbench-linux-arm64-webkitgtk.tar.gz")).toMatchObject({
    os: "Linux",
    arch: "arm64",
    target: "linux-arm64",
    webkitgtk: true,
  });
  expect(parseBuild("rtcbench-headless-windows-arm64.exe")).toMatchObject({
    variant: "headless",
    webkitgtk: false,
  });
  expect(parseBuild("rtcbench-macos-amd64.zip")).toMatchObject({
    os: "macOS",
    arch: "Intel",
  });
  expect(parseBuild("rtcbench-headless-macos-arm64.zip")).toMatchObject({
    arch: "Apple silicon",
  });
  expect(parseBuild("checksums.txt")).toBe(undefined);
  const fallback = buildsOf("rtcbench/rtcbench");
  expect(fallback).toHaveLength(12);
  expect(fallback[0]?.url).toBe(
    assetUrl("rtcbench/rtcbench", "rtcbench-linux-arm64-webkitgtk.tar.gz"),
  );
  expect(fallback.every((build) => build.published)).toBe(true);
});

it("reads the release list the site build bakes in", () => {
  const releases = parseReleases([
    { tag_name: "v1.1.0-rc.1", prerelease: true, assets: [] },
    {
      tag_name: "v1.0.0",
      draft: false,
      assets: [
        {
          name: "sha256sums.txt",
          size: 10,
          browser_download_url: "https://example.test/sums",
        },
        {
          name: "rtcbench-headless-linux-amd64.tar.gz",
          size: 14_200_000,
          browser_download_url: "https://example.test/headless",
        },
      ],
    },
    { tag_name: "v0.9.0", draft: true, assets: [] },
  ]);
  expect(releases.map((release) => release.tag)).toEqual([
    "v1.1.0-rc.1",
    "v1.0.0",
  ]);
  const builds = buildsOf("rtcbench/rtcbench", releases[1]);
  expect(builds).toHaveLength(12);
  expect(builds.find((build) => build.id === "headless-linux-amd64")).toEqual({
    ...parseBuild("rtcbench-headless-linux-amd64.tar.gz")!,
    url: "https://example.test/headless",
    size: 14_200_000,
    published: true,
  });
  expect(builds[0]).toMatchObject({
    id: "desktop-linux-arm64",
    url: "https://github.com/rtcbench/rtcbench/releases/tag/v1.0.0",
    published: false,
  });
  expect(parseReleases("nope")).toEqual([]);
});

it("groups versions by minor, newest first", () => {
  const releases = parseReleases([
    { tag_name: "v1.0.1", assets: [] },
    { tag_name: "v0.17.4", assets: [] },
    { tag_name: "v1.1.0-rc.1", prerelease: true, assets: [] },
    { tag_name: "v1.0.0", assets: [] },
    { tag_name: "v0.17.3", assets: [] },
  ]);
  expect(
    groupReleases(releases).map((group) => [
      group.minor,
      group.top.tag,
      group.others.map((release) => release.tag),
    ]),
  ).toEqual([
    ["1.1", "v1.1.0-rc.1", []],
    ["1.0", "v1.0.1", ["v1.0.0"]],
    ["0.17", "v0.17.4", ["v0.17.3"]],
  ]);
  expect(compareVersions("v1.0.0", "v1.0.0-rc.1")).toBeLessThan(0);
  expect(compareVersions("v1.0.0-rc.2", "v1.0.0-rc.10")).toBeGreaterThan(0);
  expect(parseVersion("nightly")).toBe(undefined);
  expect(headlessOnly("v0.16.2")).toBe(true);
  expect(headlessOnly("v0.17.0")).toBe(false);
  expect(headlessOnly("v1.0.0")).toBe(false);
  expect(headlessOnly("nightly")).toBe(false);
});

it("writes a curl command for the selected build", () => {
  const url = "https://example.test/x";
  expect(
    curlCommand({ file: "rtcbench-headless-linux-amd64.tar.gz", url }),
  ).toBe(`curl -fsSL ${url} | tar xz`);
  expect(curlCommand({ file: "rtcbench-macos-arm64.zip", url })).toBe(
    `curl -fsSLo rtcbench.zip ${url} && unzip -o rtcbench.zip`,
  );
  expect(curlCommand({ file: "rtcbench-windows-amd64.exe", url })).toBe(
    `curl.exe -fsSLo rtcbench.exe ${url}`,
  );
});

it("reads sha256sum and md5sum output", () => {
  const sha = "a".repeat(64);
  const md5 = "B".repeat(32);
  expect(
    parseChecksums(
      `${sha}  rtcbench-macos-arm64.zip\n${md5} *rtcbench-macos-arm64.zip\nnonsense\n${md5}  other.zip\n`,
    ),
  ).toEqual({
    "rtcbench-macos-arm64.zip": { sha256: sha, md5: md5.toLowerCase() },
    "other.zip": { md5: md5.toLowerCase() },
  });
});

it("formats download sizes", () => {
  expect(formatSize(14_200_000)).toBe("14 MB");
  expect(formatSize(400_000)).toBe("1 MB");
});

it("opens tabs from the app and links to the paper", () => {
  const html = renderToStaticMarkup(<Welcome visit={() => {}} />);
  expect(html).not.toContain("doi.org");
  expect(html).toContain('aria-expanded="false"');
  expect(html).toMatch(/<h2[^>]*><button/);
  expect(html).not.toContain("Read the paper");
  expect(html).toContain("https://www.linkedin.com/in/evanram");
  expect(html).toContain("https://2026.acmmm.org/");
  expect(html).toContain("Configs");
  expect(html).not.toContain("Download");
  expect(html).not.toContain("Open a tab");
  expect(bibtex).toContain("doi = {10.1145/3767308.3835424}");
});

it("offers the latest release on the website", () => {
  const html = renderToStaticMarkup(
    <Welcome releases="rtcbench/rtcbench" visit={() => {}} />,
  );
  expect(html).toContain("https://github.com/rtcbench/rtcbench/releases");
  expect(html).toMatch(/Download<\/span>/);
  expect(html).toContain("Star on GitHub");
  expect(html).toContain("Come say hi!");
  expect(html).toContain(
    'href="https://github.com/rtcbench/rtcbench/wiki/Discord-link"',
  );
  expect(html).toContain('href="https://github.com/rtcbench/rtcbench"');
  expect(html).not.toContain("Headless");
  expect(html).not.toContain("Configs</button>");
});

it("links the top bar to the welcome page", () => {
  const html = renderToStaticMarkup(<DownloadButton onClick={() => {}} />);
  expect(html).toContain("Download</button>");
  expect(html).not.toContain("Download for");
});
