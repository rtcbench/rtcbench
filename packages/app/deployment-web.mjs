import { build } from "esbuild";
import { access, copyFile, readdir } from "node:fs/promises";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const app = new URL("./", import.meta.url);

export const deploymentEntries = {
  deployment: "web/deployment/index.html",
  "deployment-client": "web/deployment/client.js",
};
for (const name of await readdir(new URL("../../plugin", app))) {
  const path = `../../plugin/${name}/web/client.js`;
  try {
    await access(new URL(path, app));
  } catch {
    continue;
  }
  deploymentEntries[`deployment-${name}`] = path;
}
export async function copyDeploymentLicenses(outdir) {
  for (const name of ["livekit-client", "mediasoup-client"]) {
    await copyFile(
      new URL("../LICENSE", import.meta.resolve(name)),
      `${outdir}/${name}-LICENSE`,
    );
  }
}
export async function buildDeploymentWeb(outdir, options = {}) {
  await copyDeploymentLicenses(outdir);
  return build({
    entryPoints: deploymentEntries,
    outdir,
    nodePaths: ["../node_modules"],
    bundle: true,
    format: "esm",
    splitting: true,
    legalComments: "linked",
    minify: true,
    loader: { ".html": "copy" },
    ...options,
  });
}
if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
) {
  await buildDeploymentWeb(process.argv[2]);
}
