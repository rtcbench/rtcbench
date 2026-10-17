import { build } from "esbuild";
import {
  copyFile,
  mkdir,
  readFile,
  readdir,
  rm,
  writeFile,
} from "node:fs/promises";
import { dirname } from "node:path";

const outdir = process.argv[2] ?? "../../dist/site";
const base = (process.env.SITE_BASE || "/").replace(/\/*$/, "/");
const releases = process.env.SITE_RELEASES || "rtcbench/rtcbench";
if (!/^[\w.-]+\/[\w.-]+$/.test(releases))
  throw new Error(`SITE_RELEASES must be owner/name, got ${releases}`);

await mkdir(outdir, { recursive: true });
for (const file of await readdir(outdir)) {
  if (/\.(js|map|txt|html)$/.test(file)) await rm(`${outdir}/${file}`);
}
await build({
  entryPoints: { main: "web/main.tsx" },
  nodePaths: ["../node_modules"],
  outdir,
  bundle: true,
  format: "esm",
  splitting: true,
  legalComments: "linked",
  minify: true,
  sourcemap: true,
  loader: { ".svg": "dataurl" },
  define: { "process.env.NODE_ENV": '"production"' },
});
const site = (process.env.SITE_URL || "https://rtcbench.com").replace(
  /\/$/,
  "",
);
const paper = JSON.parse(await readFile("web/paper.json", "utf8"));
const tagline =
  "Benchmark SFUs with thousands of video streams from one machine";
const title = `RTCBench: ${tagline}`;
const description =
  "RTCBench is an open-source benchmarking framework for multi-party video conferencing. It drives thousands of WebRTC streams against Jitsi, LiveKit, mediasoup and other SFUs from one machine, 86 times more than Chromium-based clients. Published at ACM Multimedia 2026.";
const escape = (text) =>
  text
    .replaceAll("&", "&amp;")
    .replaceAll('"', "&quot;")
    .replaceAll("<", "&lt;");
const authors = paper.authors.map((author) => ({
  "@type": "Person",
  name: author.name,
  url: author.url,
  affiliation: { "@type": "Organization", name: paper.affiliation },
}));
const structured = {
  "@context": "https://schema.org",
  "@graph": [
    {
      "@type": "WebSite",
      "@id": `${site}/#website`,
      url: `${site}/`,
      name: "RTCBench",
      description,
    },
    {
      "@type": "SoftwareApplication",
      "@id": `${site}/#software`,
      name: "RTCBench",
      url: `${site}/`,
      description,
      applicationCategory: "DeveloperApplication",
      operatingSystem: "Windows, macOS, Linux",
      license: `https://github.com/${releases}/blob/main/LICENSE`,
      downloadUrl: `https://github.com/${releases}/releases`,
      sameAs: [`https://github.com/${releases}`],
      offers: { "@type": "Offer", price: "0", priceCurrency: "USD" },
      author: authors,
      citation: { "@id": `${site}/#paper` },
    },
    {
      "@type": "ScholarlyArticle",
      "@id": `${site}/#paper`,
      headline: paper.title,
      name: paper.title,
      author: authors,
      datePublished: String(paper.year),
      publisher: { "@type": "Organization", name: "ACM" },
      isPartOf: { "@type": "CreativeWork", name: paper.proceedings },
      identifier: {
        "@type": "PropertyValue",
        propertyID: "DOI",
        value: paper.doi,
      },
      sameAs: `https://doi.org/${paper.doi}`,
      url: `${site}/paper`,
      license: "https://creativecommons.org/licenses/by/4.0/",
      about: { "@id": `${site}/#software` },
    },
  ],
};
const head = `<meta name="description" content="${escape(description)}" />
    <link rel="canonical" href="${site}/" />
    <meta name="robots" content="index, follow" />
    <meta property="og:type" content="website" />
    <meta property="og:site_name" content="RTCBench" />
    <meta property="og:title" content="${escape(title)}" />
    <meta property="og:description" content="${escape(description)}" />
    <meta property="og:url" content="${site}/" />
    <meta property="og:image" content="${site}/og.png" />
    <meta property="og:image:width" content="1200" />
    <meta property="og:image:height" content="630" />
    <meta property="og:image:alt" content="${escape(title)}" />
    <meta name="twitter:card" content="summary_large_image" />
    <meta name="twitter:title" content="${escape(title)}" />
    <meta name="twitter:description" content="${escape(description)}" />
    <meta name="twitter:image" content="${site}/og.png" />
    <script type="application/ld+json">${JSON.stringify(structured).replaceAll("<", "\\u003c")}</script>`;
const fallback = `<main style="max-width:48rem;margin:0 auto;padding:2.5rem 1rem;font-family:system-ui,sans-serif;line-height:1.5"><h1>RTCBench</h1><p>${tagline}</p><p>Works with Jitsi, LiveKit, mediasoup and more. Free and open source under the MIT License, for Windows, macOS and Linux.</p><p><a href="https://github.com/${releases}/releases">Download</a> &middot; <a href="https://github.com/${releases}">GitHub</a></p><h2>${escape(paper.title)}</h2><p>${paper.authors.map((author) => author.name).join(", ")}, ${paper.affiliation}. ${paper.venue}, ${paper.city}.</p></main>`;
const page = (await readFile("web/index.html", "utf8"))
  .replace('<base href="/"', `<base href="${base}"`)
  .replace(
    'data-welcome="false"',
    `data-welcome="true" data-preview="true" data-releases="${releases}"`,
  )
  .replace("<title>RTCBench</title>", `<title>${escape(title)}</title>`)
  .replace(/<meta\s+name="description"[^>]*\/>/, head)
  .replace('<div id="root"></div>', `<div id="root">${fallback}</div>`);
await writeFile(`${outdir}/index.html`, page);
await writeFile(`${outdir}/404.html`, page);
await writeFile(`${outdir}/.nojekyll`, "");
await writeFile(
  `${outdir}/robots.txt`,
  `User-agent: *\nAllow: /\nSitemap: ${site}/sitemap.xml\n`,
);
await writeFile(
  `${outdir}/sitemap.xml`,
  `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>${site}/</loc><lastmod>${new Date().toISOString().slice(0, 10)}</lastmod></url></urlset>\n`,
);
await copyFile("../../docs/media/logo_icon.svg", `${outdir}/logo_icon.svg`);
await copyFile("web/og.png", `${outdir}/og.png`);
await copyFile(
  new URL("../LICENSE", import.meta.resolve("@wterm/dom")),
  `${outdir}/wterm-LICENSE`,
);
const examplesDir = "../../config-examples";
const examples = (await readdir(examplesDir, { recursive: true }))
  .filter((file) => file.endsWith(".yml"))
  .map((file) => file.replaceAll("\\", "/"))
  .sort();
for (const file of examples) {
  await mkdir(dirname(`${outdir}/examples/${file}`), { recursive: true });
  await copyFile(`${examplesDir}/${file}`, `${outdir}/examples/${file}`);
}
await writeFile(`${outdir}/examples/index.json`, JSON.stringify(examples));

const redirects = JSON.parse(await readFile("redirects.json", "utf8"));
for (const [path, url] of Object.entries(redirects)) {
  if (!url) continue;
  const target = new URL(url).href;
  await mkdir(`${outdir}/${path}`, { recursive: true });
  await writeFile(
    `${outdir}/${path}/index.html`,
    `<!doctype html><meta charset="utf-8"><meta http-equiv="refresh" content="0; url=${target}"><link rel="canonical" href="${target}"><title>RTCBench</title><a href="${target}">${target}</a>\n`,
  );
}
