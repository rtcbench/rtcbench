import { beforeAll, expect, it } from "bun:test";
import { build } from "esbuild";
import { mkdtempSync, readFileSync, readdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { gzipSync } from "node:zlib";
import postcss from "postcss";
import tailwind3 from "tailwindcss3";
import { unlayer } from "./unlayer.mjs";

const temp = <T>(run: (dir: string) => T) => {
  const dir = mkdtempSync(join(tmpdir(), "rtcbench-"));
  try {
    return run(dir);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
};

export const buildStyles = (dir: string, input = "styles.css") =>
  temp((out) => {
    const output = join(out, "styles.css");
    const cli = fileURLToPath(
      new URL(
        "./dist/index.mjs",
        import.meta.resolve("@tailwindcss/cli/package.json"),
      ),
    );
    const { exitCode, stderr } = Bun.spawnSync(
      [process.execPath, cli, "-i", input, "-o", output, "--minify"],
      { cwd: dir },
    );
    expect(exitCode, stderr.toString()).toBe(0);
    return unlayer(readFileSync(output, "utf8"));
  });

export function testStyles(
  dir: string,
  {
    prefix,
    input,
    global = /$^/,
  }: { prefix: string; input?: string; global?: RegExp },
) {
  let css = "";
  beforeAll(() => {
    css = buildStyles(dir, input);
  });
  const own = `.${prefix}\\:bg-card{`;

  it("ships scoped rules without cascade layers", () => {
    expect(
      [...css.matchAll(/@layer\s*([^{;]*)/g)].map((m) => m[1]!.trim()),
    ).toEqual(["properties"]);
    const exempt = (node: postcss.Node): boolean => {
      const parent = node.parent as postcss.Node | undefined;
      if (!parent) return false;
      const at = parent.type === "atrule" ? (parent as postcss.AtRule) : null;
      return (
        at?.params === "properties" ||
        at?.name === "keyframes" ||
        exempt(parent)
      );
    };
    const unscoped: string[] = [];
    postcss.parse(css).walkRules((rule) => {
      if (!exempt(rule))
        for (const selector of rule.selectors)
          if (
            !selector.includes(`${prefix}\\:`) &&
            !/^(:root|:host|\.dark|:where\(:root\)|:where\(\.dark\))$/.test(
              selector,
            ) &&
            !global.test(selector)
          )
            unscoped.push(selector);
    });
    expect(unscoped).toEqual([]);
    const reset = css.indexOf(`:where([class*=${prefix}\\:])`);
    expect(reset).toBeGreaterThan(-1);
    expect(reset).toBeLessThan(css.indexOf(own));
    const keyframes = [...css.matchAll(/@keyframes\s+([\w-]+)/g)].map(
      (m) => m[1]!,
    );
    expect(
      keyframes.filter((n) => !n.startsWith(`${prefix}-`) && !global.test(n)),
    ).toEqual([]);
  });

  it("builds through a Tailwind 3 PostCSS pipeline", async () => {
    const content = [{ raw: '<button class="p-4">' }];
    const alone = await postcss([tailwind3({ content })]).process(css, {
      from: "package.css",
    });
    expect(alone.css).toContain(own);
    const app = await postcss([tailwind3({ content })]).process(
      `@tailwind base;\n@tailwind components;\n@tailwind utilities;\n${css}`,
      { from: "app.css" },
    );
    expect(app.css).toContain(".p-4");
    expect(app.css).toContain(own);
  });

  it("lets apps override tokens while forced colors win", () => {
    expect(
      [...css.matchAll(/(--[\w-]+):\s*var\(\1\)/g)].map((m) => m[0]),
    ).toEqual([]);
    const defaults = css.indexOf(`:where(:root){--${prefix}-`);
    const dark = css.indexOf(`:where(.dark){--${prefix}-background:`);
    const forced = css.indexOf(
      `@media (forced-colors:active){:root,.dark{--${prefix}-background:Canvas!important;`,
    );
    expect(defaults).toBeGreaterThan(-1);
    expect(dark).toBeGreaterThan(defaults);
    expect(forced).toBeGreaterThan(dark);
  });

  return () => css;
}

export function testPackage(
  dir: string,
  {
    dependencies,
    peers,
    entries = ["index.ts"],
    budget,
  }: {
    dependencies: string[];
    peers: Record<string, string>;
    entries?: string[];
    budget: number;
  },
) {
  const manifest = JSON.parse(readFileSync(join(dir, "package.json"), "utf8"));
  const shared = [...Object.keys(peers), ...dependencies];
  const declared = (path: string) =>
    shared.some((name) => path === name || path.startsWith(name + "/"));
  const bundle = (options: Parameters<typeof build>[0]) =>
    build({
      entryPoints: entries.map((entry) => join(dir, entry)),
      bundle: true,
      format: "esm",
      write: false,
      logLevel: "silent",
      outdir: "out",
      loader: { ".svg": "dataurl" },
      ...options,
    });

  it("shares React and its other peers with the host app", () => {
    expect(Object.keys(manifest.dependencies ?? {})).toEqual(dependencies);
    expect(manifest.peerDependencies).toEqual(peers);
  });

  it("imports only its dependencies and peers", async () => {
    const { metafile } = await bundle({ packages: "external", metafile: true });
    const imports = Object.values(metafile!.outputs).flatMap((output) =>
      output.imports.filter((i) => i.external).map((i) => i.path),
    );
    expect(imports.filter((path) => !declared(path))).toEqual([]);
  });

  it("adds little to an app bundle beyond the peers it shares", async () => {
    const { outputFiles } = await bundle({
      minify: true,
      splitting: entries.length > 1,
      external: Object.keys(peers).flatMap((name) => [name, name + "/*"]),
    });
    const size = outputFiles!
      .filter((file) => file.path.endsWith(".js"))
      .reduce((total, file) => total + gzipSync(file.contents).length, 0);
    expect(size).toBeLessThan(budget);
  });

  it("publishes declarations without the radix-ui package", () =>
    temp((out) => {
      const { exitCode, stdout } = Bun.spawnSync(
        [process.execPath, "x", "tsc", "-p", "tsconfig.json", "--outDir", out],
        { cwd: dir },
      );
      expect(exitCode, stdout.toString()).toBe(0);
      const files = readdirSync(out, { recursive: true, encoding: "utf8" })
        .filter((file) => file.endsWith(".d.ts"))
        .map((file) => readFileSync(join(out, file), "utf8"));
      expect(files.length).toBeGreaterThan(0);
      for (const source of files)
        expect(source).not.toMatch(/from "radix-ui"/);
    }));
}
