import { resolve } from "node:path";
import { context } from "esbuild";
import { deploymentEntries } from "./deployment-web.mjs";
import { fileURLToPath } from "node:url";
import { dockerContext, dockerPlugins } from "./docker-contexts.mjs";

const app = import.meta.dirname;
const file = (path) => Bun.file(resolve(app, path));
const children = new Set();
const stop = () => {
  for (const child of children) child.kill();
};
process.on("SIGINT", stop);
process.on("SIGTERM", stop);
const spawn = (cmd, options = {}) => {
  const child = Bun.spawn(cmd, {
    cwd: app,
    stdout: "inherit",
    stderr: "inherit",
    ...options,
  });
  children.add(child);
  child.exited.then(() => children.delete(child));
  return child;
};
const tailwind = fileURLToPath(
  new URL(
    "./dist/index.mjs",
    import.meta.resolve("@tailwindcss/cli/package.json"),
  ),
);
const styles = [
  ["web/styles.css", "dist/styles.css"],
  ["web/standalone.css", "dist/dev/styles.css"],
];
let server, deployment;
try {
  for (const [input, output] of styles) {
    const command = [
      process.execPath,
      tailwind,
      "-i",
      input,
      "-o",
      output,
      "--map",
    ];
    if (await spawn(command).exited)
      throw new Error(`Could not build ${input}`);
    spawn([...command, "--watch=always"]);
  }
  const html = (await file("web/index.html").text())
    .replace(
      'href="logo_icon.svg"',
      'href="../../../../docs/media/logo_icon.svg"',
    )
    .replace('src="main.js"', 'src="./main.tsx"');
  await Bun.write(file("dist/dev/index.html"), html);
  await Bun.write(
    file("dist/dev/main.tsx"),
    'import "../../web/main.tsx";\n',
  );
  const settings = resolve(app, "../../rtcbench-data/rtcbench-dev.yml");
  if (!(await Bun.file(settings).exists())) {
    await Bun.write(settings, "ui:\n  address: http://127.0.0.1:3000\n");
  }
  deployment = await context({
    entryPoints: deploymentEntries,
    outdir: resolve(app, "dist/dev/deployment"),
    absWorkingDir: app,
    nodePaths: [resolve(app, "../node_modules")],
    bundle: true,
    format: "esm",
    loader: { ".html": "copy" },
    sourcemap: "inline",
  });
  await deployment.watch();
  await deployment.rebuild();
  const { default: index } = await import("./dist/dev/index.html");
  server = Bun.serve({
    hostname: "127.0.0.1",
    port: 3000,
    development: true,
    routes: {
      ...Object.fromEntries(
        Object.keys(deploymentEntries).map((name) => [
          `/${name}.${name === "deployment" ? "html" : "js"}`,
          () =>
            new Response(
              file(`dist/dev/deployment/${name}.${name === "deployment" ? "html" : "js"}`),
            ),
        ]),
      ),
      ...Object.fromEntries(
        dockerPlugins.map((plugin) => [
          `/docker-${plugin}.json`,
          async () => Response.json(await dockerContext(plugin)),
        ]),
      ),
      "/*": index,
    },
  });
  spawn(
    [
      resolve(
        app,
        `../../bin/rtcbench-dev${process.platform === "win32" ? ".exe" : ""}`,
      ),
      "-settings",
      settings,
      ...process.argv.slice(2),
    ],
    { cwd: resolve(app, "../..") },
  );
  process.exitCode = await Promise.race(
    [...children].map((child) => child.exited),
  );
} finally {
  stop();
  await server?.stop(true);
  await deployment?.dispose();
}
