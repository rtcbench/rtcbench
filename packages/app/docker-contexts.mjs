import { mkdir, readFile, readdir, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { parse } from "yaml";

export async function discoverDockerContexts(root) {
  const sources = new Map();
  for (const entry of await readdir(root, { withFileTypes: true })) {
    if (!entry.isDirectory()) continue;
    const manifest = resolve(root, entry.name, "plugin.yml");
    const plugin = parse(await readFile(manifest, "utf8"));
    if (!plugin.build_context?.length) continue;
    if (sources.has(plugin.id))
      throw new Error(`Duplicate plugin: ${plugin.id}`);
    sources.set(plugin.id, {
      directory: dirname(resolve(dirname(manifest), plugin.dockerfile)),
      files: plugin.build_context,
    });
  }
  return sources;
}

const sources = await discoverDockerContexts(
  fileURLToPath(new URL("../../plugin", import.meta.url)),
);
export const dockerPlugins = [...sources.keys()];

export async function dockerContext(plugin) {
  const source = sources.get(plugin);
  if (!source) throw new Error("Unknown SFU");
  return Object.fromEntries(
    await Promise.all(
      source.files.map(async (name) => [
        `${plugin}/${name}`,
        await readFile(resolve(source.directory, name), "utf8"),
      ]),
    ),
  );
}

export async function writeDockerContexts(outdir) {
  await mkdir(outdir, { recursive: true });
  for (const plugin of dockerPlugins) {
    await writeFile(
      resolve(outdir, `docker-${plugin}.json`),
      JSON.stringify(await dockerContext(plugin)),
    );
  }
}

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  await writeDockerContexts(process.argv[2] ?? "dist");
}
