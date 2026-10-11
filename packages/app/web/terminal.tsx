import { useContext, useEffect, useRef, useState } from "react";
import { WTerm } from "@wterm/dom";
import { RotateCwIcon, XIcon } from "lucide-react";
import { Button } from "@rtcbench/admin";
import { useApi } from "./api";
import { PageTab, usePageTitle } from "./integration";

const light = {
  foreground: 0x262626,
  background: 0xffffff,
  cursor: 0x262626,
  palette: [
    0x262626, 0xb42318, 0x237a36, 0x876000, 0x245ac0, 0x8b3fa0, 0x006e7a,
    0xd4d4d4, 0x616161, 0xc43326, 0x287d3c, 0x8a6500, 0x2563c7, 0x9542aa,
    0x007c8a, 0xffffff,
  ],
};
const dark = {
  foreground: 0xe5e5e5,
  background: 0x262626,
  cursor: 0xe5e5e5,
  palette: [
    0x1e1e1e, 0xf44747, 0x6a9955, 0xd7ba7d, 0x569cd6, 0xc586c0, 0x4ec9b0,
    0xd4d4d4, 0x808080, 0xff6b6b, 0x8ec07c, 0xffd866, 0x82aaff, 0xd7a1d7,
    0x7fdbca, 0xffffff,
  ],
};

export default function Terminal({ target = "" }: { target?: string }) {
  const { request, url } = useApi();
  const refresh = useContext(PageTab)?.refresh;
  const reconnect = useRef(() => {});
  const [name, setName] = useState("");
  const [status, setStatus] = useState("Connecting…");
  const [error, setError] = useState("");
  const host = useRef<HTMLDivElement>(null);
  usePageTitle(name || "Terminal");
  const [kind, id] = target.split("/");
  const query = new URLSearchParams(
    kind === "plugins" ? { project: id! } : kind ? { kind, id: id! } : {},
  );
  const endpoint = "/terminal?" + query;

  useEffect(() => {
    refresh?.(() => reconnect.current());
    return () => refresh?.();
  }, [refresh]);

  useEffect(() => {
    let disposed = false,
      ws: WebSocket | undefined,
      initialized: ReturnType<WTerm["init"]> | undefined,
      retryTimer: ReturnType<typeof setTimeout> | undefined,
      generation = 0;
    const disconnect = () => {
      clearTimeout(retryTimer);
      if (ws) {
        ws.onopen = ws.onmessage = ws.onclose = ws.onerror = null;
        ws.close();
        ws = undefined;
      }
    };
    const send = (bytes: Uint8Array) => {
      if (ws?.readyState !== WebSocket.OPEN) return;
      if (ws.bufferedAmount + bytes.length > 1024 * 1024) {
        setError("Terminal input buffer is full; input was not sent");
        return;
      }
      for (let i = 0; i < bytes.length; i += 16384)
        ws.send(bytes.slice(i, i + 16384));
    };
    const resize = (cols: number, rows: number) => {
      if (ws?.readyState === WebSocket.OPEN)
        ws.send(JSON.stringify({ cols, rows }));
    };
    const terminal = new WTerm(host.current!, {
      onData: (text) => send(new TextEncoder().encode(text)),
      onBinary: send,
      onResize: resize,
      announceOutput: true,
    });
    let currentTheme: typeof light | undefined;
    const updateTheme = () => {
      if (!host.current) return;
      const next =
        getComputedStyle(host.current).colorScheme === "dark" ? dark : light;
      if (next !== currentTheme) terminal.setThemeColors(next);
      currentTheme = next;
    };
    const theme = new MutationObserver(updateTheme);
    for (
      let parent = host.current!.parentElement;
      parent;
      parent = parent.parentElement
    )
      theme.observe(parent, {
        attributes: true,
        attributeFilter: ["class", "style"],
      });
    const preferredTheme = matchMedia("(prefers-color-scheme: dark)");
    preferredTheme.addEventListener("change", updateTheme);
    updateTheme();
    const visible = new ResizeObserver(() =>
      terminal.setRenderingPaused(!host.current?.clientHeight),
    );
    visible.observe(host.current!);
    const connect = async (canRetry = true) => {
      const current = ++generation;
      const stale = () => disposed || current !== generation;
      disconnect();
      setError("");
      setStatus(canRetry ? "Connecting…" : "Reconnecting…");
      const failed = (message: string) => {
        if (stale()) return;
        if (canRetry) {
          setStatus("Reconnecting…");
          retryTimer = setTimeout(() => void connect(false), 1000);
        } else {
          setStatus("Disconnected");
          setError(message);
        }
      };
      try {
        const value = await request<{ name: string }>("GET", endpoint);
        if (stale()) return;
        setName(value.name);
        initialized ??= terminal.init();
        await initialized;
        if (stale()) return;
        const address = new URL(url(endpoint), document.baseURI);
        address.protocol = address.protocol === "https:" ? "wss:" : "ws:";
        ws = new WebSocket(address);
        ws.binaryType = "arraybuffer";
        let connectionError = "";
        ws.onopen = () => {
          setStatus("Connected");
          resize(terminal.cols, terminal.rows);
        };
        ws.onmessage = (event) => {
          if (typeof event.data === "string") connectionError = event.data;
          else {
            canRetry = true;
            terminal.write(new Uint8Array(event.data));
          }
        };
        ws.onerror = () => {
          connectionError = "Could not connect to the terminal";
        };
        ws.onclose = (event) => {
          if (event.code === 1000 && !connectionError)
            setStatus("Disconnected");
          else failed(connectionError || event.reason || "Connection lost");
        };
      } catch (err) {
        failed(err instanceof Error ? err.message : String(err));
      }
    };
    reconnect.current = () => {
      void connect();
      terminal.focus();
    };
    void connect();
    return () => {
      disposed = true;
      reconnect.current = () => {};
      disconnect();
      visible.disconnect();
      theme.disconnect();
      preferredTheme.removeEventListener("change", updateTheme);
      terminal.destroy();
    };
  }, [endpoint, request, url]);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <span role="status" className="sr-only">
        {status}
      </span>
      {(status === "Disconnected" || error) && (
        <div
          key="banner"
          role="alert"
          className="flex shrink-0 items-center gap-2 border-b bg-muted px-3 py-1 text-xs"
        >
          <span className="min-w-0 flex-1">
            {error || "Terminal disconnected"}
          </span>
          <Button
            variant="ghost"
            size="icon-xs"
            aria-label={
              status === "Connected"
                ? "Dismiss terminal message"
                : "Reconnect terminal"
            }
            title={status === "Connected" ? "Dismiss" : "Reconnect terminal"}
            onClick={() =>
              status === "Connected" ? setError("") : reconnect.current()
            }
          >
            {status === "Connected" ? (
              <XIcon aria-hidden="true" />
            ) : (
              <RotateCwIcon aria-hidden="true" />
            )}
          </Button>
        </div>
      )}
      <div
        key="terminal"
        ref={host}
        aria-label="Terminal input"
        className="min-h-0 flex-1"
        style={{ borderRadius: 0, boxShadow: "none" }}
      />
    </div>
  );
}
