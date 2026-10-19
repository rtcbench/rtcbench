import { resolve, sep } from "node:path";

const root = resolve(import.meta.dir, "../../../dist/site");

Bun.serve({
  hostname: "127.0.0.1",
  port: 18728,
  async fetch(request) {
    const path = resolve(
      root,
      "." + decodeURIComponent(new URL(request.url).pathname),
    );
    if (path !== root && !path.startsWith(root + sep))
      return new Response(null, { status: 403 });
    const file = Bun.file(path);
    return new Response(
      (await file.exists()) ? file : Bun.file(resolve(root, "index.html")),
    );
  },
});
