import { expect, it } from "bun:test";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  discoverDockerContexts,
  dockerPlugins,
  writeDockerContexts,
} from "../docker-contexts.mjs";

it("packages every Docker build context as a standalone UI asset", async () => {
  const dir = await mkdtemp(join(tmpdir(), "rtcbench-contexts-"));
  try {
    await writeDockerContexts(dir);
    expect([...dockerPlugins].sort()).toEqual([
      "janus",
      "jitsi",
      "livekit",
      "mediasoup",
    ]);
    for (const plugin of dockerPlugins) {
      const files = JSON.parse(
        await readFile(join(dir, `docker-${plugin}.json`), "utf8"),
      );
      expect(files[`${plugin}/Dockerfile`]).toContain("FROM ");
      for (const [path, content] of Object.entries(files)) {
        expect(path.startsWith(`${plugin}/`)).toBe(true);
        expect(content).toBe(
          await readFile(
            new URL(`../../../docker/${path}`, import.meta.url),
            "utf8",
          ),
        );
      }
    }
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

it("discovers a custom plugin's Docker context from its manifest", async () => {
  const root = await mkdtemp(join(tmpdir(), "rtcbench-plugin-"));
  try {
    await mkdir(join(root, "custom"));
    await writeFile(
      join(root, "custom/plugin.yml"),
      "id: custom\nname: Custom\ndockerfile: docker/Dockerfile\nbuild_context: [Dockerfile, server.conf]\n",
    );
    const sources = await discoverDockerContexts(root);
    expect([...sources.keys()]).toEqual(["custom"]);
    expect(sources.get("custom")).toEqual({
      directory: join(root, "custom/docker"),
      files: ["Dockerfile", "server.conf"],
    });
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
