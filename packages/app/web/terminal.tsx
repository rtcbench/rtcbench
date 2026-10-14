import { useCallback, useContext, useEffect, useRef, useState } from "react";
import { Terminal as Shell, type TerminalHandle } from "@labpane/ui/terminal";
import { useApi } from "./api";
import { PageTab, usePageTitle } from "./integration";
import { useTerminalTheme } from "./terminal-theme";

export default function Terminal({ target = "" }: { target?: string }) {
  const { request, url } = useApi();
  const refresh = useContext(PageTab)?.refresh;
  const terminal = useRef<TerminalHandle>(null);
  const [name, setName] = useState("");
  const theme = useTerminalTheme();
  usePageTitle(name || "Terminal");
  const [kind, id] = target.split("/");
  const query = new URLSearchParams(
    kind === "plugins" ? { project: id! } : kind ? { kind, id: id! } : {},
  );
  const endpoint = "/terminal?" + query;

  useEffect(() => {
    refresh?.(() => terminal.current?.reconnect());
    return () => refresh?.();
  }, [refresh]);

  const connect = useCallback(async () => {
    setName((await request<{ name: string }>("GET", endpoint)).name);
    return url(endpoint);
  }, [endpoint, request, url]);

  return <Shell ref={terminal} connect={connect} theme={theme} expandable />;
}
