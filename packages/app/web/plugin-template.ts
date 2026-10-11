import file0 from "../plugin-template/.dockerignore.tmpl" with { type: "text" };
import file1 from "../plugin-template/.gitignore.tmpl" with { type: "text" };
import file2 from "../plugin-template/benchmark.yml.tmpl" with { type: "text" };
import file3 from "../plugin-template/Dockerfile.tmpl" with { type: "text" };
import file4 from "../plugin-template/go.mod.tmpl" with { type: "text" };
import file5 from "../plugin-template/main.go.tmpl" with { type: "text" };
import file6 from "../plugin-template/Makefile.tmpl" with { type: "text" };
import file7 from "../plugin-template/plugin/plugin.go.tmpl" with { type: "text" };
import file8 from "../plugin-template/plugin.yml.tmpl" with { type: "text" };
import file9 from "../plugin-template/rtcbench.yml.tmpl" with { type: "text" };
import file10 from "../plugin-template/web/build.mjs.tmpl" with { type: "text" };
import file11 from "../plugin-template/web/index.html.tmpl" with { type: "text" };
import file12 from "../plugin-template/web/main.tsx.tmpl" with { type: "text" };
import file13 from "../plugin-template/web/package.json.tmpl" with { type: "text" };
import file14 from "../plugin-template/web/styles.css.tmpl" with { type: "text" };
import file15 from "../plugin-template/web/styles.d.ts.tmpl" with { type: "text" };

const templates = {
  ".dockerignore": file0,
  ".gitignore": file1,
  "benchmark.yml": file2,
  Dockerfile: file3,
  "go.mod": file4,
  "main.go": file5,
  Makefile: file6,
  "plugin/plugin.go": file7,
  "plugin.yml": file8,
  "rtcbench.yml": file9,
  "web/build.mjs": file10,
  "web/index.html": file11,
  "web/main.tsx": file12,
  "web/package.json": file13,
  "web/styles.css": file14,
  "web/styles.d.ts": file15,
};

export function pluginFiles(name: string, module: string) {
  return Object.fromEntries(
    Object.entries(templates).map(([path, source]) => [
      path,
      source.replaceAll("__PLUGIN__", name).replaceAll("__MODULE__", module),
    ]),
  );
}
