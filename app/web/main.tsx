import { render } from "preact";
import { useState } from "react";
import { MoonIcon, SunIcon } from "lucide-react";
import { pages, usePoll, useRoute } from "./api";
import { ConfigPage, ConfigsPage } from "./configs";
import { EnvPage, EnvsPage } from "./envs";
import { RunPage } from "./run";
import { RunsPage } from "./runs";
import { Button, cn, Logo, Wordmark } from "./ui";

const glass = "bg-background/70 backdrop-blur-xl backdrop-saturate-150";

function ThemeToggle() {
  const [dark, setDark] = useState(false);
  return (
    <Button
      variant="ghost"
      size="icon-sm"
      aria-label={dark ? "Light" : "Dark"}
      onClick={() => {
        setDark(!dark);
        document.documentElement.classList.toggle("dark", !dark);
      }}
    >
      {dark ? <SunIcon /> : <MoonIcon />}
    </Button>
  );
}

function Version() {
  const { data } = usePoll<{ version: string }>("/version");
  return data ? (
    <span
      className={cn(
        glass,
        "rounded-md px-2 py-1 font-mono text-xs text-muted-foreground",
      )}
    >
      {data.version}
    </span>
  ) : null;
}

function NavBar({ page }: { page: string }) {
  return (
    <div className="pointer-events-none sticky top-3 z-20 flex items-center justify-between gap-4">
      <div className="flex min-w-0 items-center gap-4">
        <a
          href="/runs"
          aria-label="RTCBench"
          className="pointer-events-auto flex shrink-0 items-baseline gap-[0.3em] text-2xl leading-none"
        >
          <Logo className="size-[0.8em] translate-y-[0.05em]" />
          <Wordmark>RTCBench</Wordmark>
        </a>
        <nav
          aria-label="Pages"
          className={cn(
            glass,
            "pointer-events-auto flex min-w-0 items-center rounded-xl border border-border p-1",
          )}
          style={{
            boxShadow:
              "0 14px 28px -18px rgb(0 0 0 / 0.55), 0 2px 6px -3px rgb(0 0 0 / 0.12)",
          }}
        >
          <div className="flex min-w-0 gap-1 overflow-x-auto">
            {Object.entries(pages).map(([key, label]) => (
              <a
                key={key}
                href={`/${key}`}
                className={cn(
                  "rounded-md px-3 py-1.5 text-sm font-medium whitespace-nowrap hover:bg-muted",
                  page === key && "bg-muted",
                )}
              >
                {label}
              </a>
            ))}
          </div>
        </nav>
      </div>
      <div className="pointer-events-auto flex shrink-0 items-center gap-2">
        <Version />
        <ThemeToggle />
      </div>
    </div>
  );
}

export function App() {
  const { page, id } = useRoute();
  return (
    <div className="mx-auto flex min-h-svh max-w-[100rem] flex-col gap-8 px-6 pt-3 pb-24 lg:px-10">
      <NavBar page={page} />
      <main>
        {page === "run" && id ? (
          <RunPage key={id} id={id} />
        ) : page === "config" && id ? (
          <ConfigPage key={id} name={id} />
        ) : page === "configs" && id === "new" ? (
          <ConfigPage key="new" />
        ) : page === "configs" ? (
          <ConfigsPage />
        ) : page === "environment" && id ? (
          <EnvPage key={id} name={id} />
        ) : page === "environments" && id === "new" ? (
          <EnvPage key="new" />
        ) : page === "environments" ? (
          <EnvsPage />
        ) : (
          <RunsPage />
        )}
      </main>
    </div>
  );
}

render(<App />, document.getElementById("root")!);
