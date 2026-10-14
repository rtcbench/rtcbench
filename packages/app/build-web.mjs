import { build } from "esbuild";
import {
  deploymentEntries,
  copyDeploymentLicenses,
} from "./deployment-web.mjs";
import { copyFile, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { isAbsolute, relative } from "node:path";
import { gzipSync } from "node:zlib";
import { writeDockerContexts } from "./docker-contexts.mjs";

const outdir = "../../internal/api/web";
for (const file of await readdir(outdir)) {
  if (file.endsWith(".js") || file.endsWith(".gz"))
    await rm(`${outdir}/${file}`);
}
await build({
  entryPoints: {
    main: "web/main.tsx",
    index: "web/index.html",
    ...deploymentEntries,
  },
  nodePaths: ["../node_modules"],
  outdir,
  bundle: true,
  format: "esm",
  splitting: true,
  legalComments: "linked",
  minify: true,
  sourcemap: true,
  loader: { ".html": "copy", ".svg": "dataurl" },
  define: { "process.env.NODE_ENV": '"production"' },
});
await copyFile("../../docs/media/logo_icon.svg", `${outdir}/logo_icon.svg`);
await copyFile(
  new URL("../LICENSE", import.meta.resolve("@wterm/dom")),
  `${outdir}/wterm-LICENSE`,
);
await writeDockerContexts(outdir);
await copyDeploymentLicenses(outdir);
for (const file of await readdir(outdir)) {
  if (file === "index.html" || file.startsWith(".")) continue;
  const path = `${outdir}/${file}`;
  let data = await readFile(path);
  if (file.endsWith(".map")) {
    const map = JSON.parse(data);
    map.sources = map.sources.map((source) =>
      isAbsolute(source)
        ? relative(outdir, source).replaceAll("\\", "/")
        : source,
    );
    map.sourcesContent = map.sourcesContent.map((content, index) =>
      map.sources[index].includes("/node_modules/") ? null : content,
    );
    data = JSON.stringify(map);
  }
  await writeFile(`${path}.gz`, gzipSync(data, { level: 9 }));
  await rm(path);
}
