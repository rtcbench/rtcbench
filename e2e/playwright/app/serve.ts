import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { resolve, join } from "node:path";

const data = await mkdtemp(join(tmpdir(), "rtcbench-browser-"));
await Bun.write(join(data, "rtcbench.yml"), "welcome: true\n");
const binary = resolve(
  import.meta.dir,
  "../../..",
  process.platform === "win32" ? "rtcbench.exe" : "rtcbench",
);
const child = Bun.spawn(
  [binary, "-data", data, "-api-addr", "127.0.0.1:18726"],
  {
    stdout: "inherit",
    stderr: "inherit",
  },
);

const server = Bun.serve({
  hostname: "127.0.0.1",
  port: 18727,
  async fetch(request) {
    const url = new URL(request.url);
    if (!url.pathname.startsWith("/app/"))
      return new Response(null, { status: 404 });
    const headers = new Headers(request.headers);
    headers.set("X-Forwarded-Prefix", "/app");
    headers.set("Accept-Encoding", "identity");
    try {
      return await fetch(
        "http://127.0.0.1:18726" + url.pathname.slice(4) + url.search,
        {
          method: request.method,
          headers,
          body: request.body,
          redirect: "manual",
        },
      );
    } catch {
      return new Response("API unavailable", { status: 503 });
    }
  },
});

let stopping = false;
async function stop(code = 0) {
  if (stopping) return;
  stopping = true;
  await server.stop(true);
  child.kill();
  await child.exited;
  await rm(data, { recursive: true, force: true });
  process.exit(code);
}
process.on("SIGTERM", () => void stop());
process.on("SIGINT", () => void stop());
void child.exited.then((code) => stop(code || 1));
