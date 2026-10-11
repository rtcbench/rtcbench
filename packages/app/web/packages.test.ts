import { expect, it } from "bun:test";
import { build } from "esbuild";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

for (const module of ["lab", "admin"]) {
  it(`${module} does not depend on app pages or the other UI module`, async () => {
    const result = await build({
      entryPoints: ["index.ts", "styles.css"].map((file) =>
        fileURLToPath(new URL(`../../${module}/${file}`, import.meta.url)),
      ),
      outdir: "dist",
      bundle: true,
      write: false,
      metafile: true,
      packages: "external",
      format: "esm",
      logLevel: "silent",
    });
    for (const input of Object.keys(result.metafile!.inputs)) {
      expect(resolve(input).replaceAll("\\", "/")).toMatch(
        new RegExp(`packages/${module}/`),
      );
    }
  });
}
