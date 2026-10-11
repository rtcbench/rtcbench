export function pluginPrompt(name: string, path: string, downloadUrl: string) {
  const server = new URL(downloadUrl);
  server.pathname = server.pathname.replace(
    /\/api\/v1\/plugins\/[^/]+\/download$/,
    "/",
  );
  return `Help me build the RTCBench plugin ${JSON.stringify(name)}.

RTCBench server: ${server.href}
Project location on that server: ${JSON.stringify(path)}
Project download: ${downloadUrl}

Clone https://github.com/rtcbench/rtcbench into a sibling rtcbench directory, or use an existing checkout. Read its plugin interfaces, built-in plugins, and build setup to understand how this project should integrate. Check that the checkout supports the generated project before changing it.

Locate my plugin project first. The path above belongs to the RTCBench server and may be remote; use the downloaded project locally or the remote project if you already have access. If neither is accessible, ask me where I extracted it or how to connect.

Ask what SFU or service I want to support and how it should connect, unless I have already explained. Implement it in plugin/, keep the existing entry point and frontend integration, and use the generated Makefile or Dockerfile to build and test. Keep changes small and explain how to run the plugin.`;
}
