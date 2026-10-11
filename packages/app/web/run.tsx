import { useNavigation, usePageTitle, Back, Link } from "./integration";
import { systemPath } from "./systems/routes";

import { type FormEvent, useMemo, useState } from "react";
import { parseDocument } from "yaml";
import type { Inventory } from "./systems/model";
import {
  CheckIcon,
  CircleStopIcon,
  FileCodeIcon,
  ExternalLinkIcon,
  PencilIcon,
  RotateCcwIcon,
  Trash2Icon,
  VariableIcon,
  XIcon,
} from "lucide-react";
import {
  type Doc,
  type DocKind,
  docPath,
  duration,
  elapsed,
  useApi,
  type Run,
  type RunFile,
  runPath,
  time,
  useNow,
  usePoll,
  validName,
  type Viewers,
} from "./api";
import {
  ConfirmDialog,
  Alert,
  AlertDescription,
  AlertTitle,
  Button,
  CopyButton,
  Input,
  Page,
  PageActions,
  Spinner,
} from "@rtcbench/admin";
import {
  FilesSection,
  LogsSection,
  SummarySection,
  ViewersSection,
} from "./run-sections";
import { NewRun, RunStatus } from "./runs";

import { YamlView } from "./yaml";

function RunName({
  run,
  path,
  onSaved,
}: {
  run: Run;
  path: string;
  onSaved: () => void;
}) {
  const { request } = useApi();
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState(run.name);
  const [saving, setSaving] = useState(false);
  const valid = validName(name.trim());

  async function save(event: FormEvent) {
    event.preventDefault();
    setSaving(true);
    try {
      await request("PATCH", path, { name });
      onSaved();
      setEditing(false);
    } finally {
      setSaving(false);
    }
  }

  if (!editing)
    return (
      <span className="flex items-center gap-1">
        <h1 className="font-display text-2xl font-bold tracking-tight">
          {run.name}
        </h1>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Rename"
          onClick={() => {
            setName(run.name);
            setEditing(true);
          }}
        >
          <PencilIcon />
        </Button>
      </span>
    );

  return (
    <form className="flex items-center gap-2" onSubmit={save}>
      <Input
        aria-label="Name"
        autoFocus
        className="w-72"
        aria-invalid={!valid}
        value={name}
        onChange={(event) => setName(event.target.value)}
        onKeyDown={(event) => event.key === "Escape" && setEditing(false)}
      />
      <Button
        type="submit"
        size="icon-sm"
        aria-label="Save"
        disabled={!valid || saving}
      >
        {saving ? <Spinner /> : <CheckIcon />}
      </Button>
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        aria-label="Cancel"
        onClick={() => setEditing(false)}
      >
        <XIcon />
      </Button>
    </form>
  );
}

function DocLink({ kind, name }: { kind: DocKind; name?: string }) {
  const { data: docs } = usePoll<Doc[]>(name ? `/${kind}s` : null);
  if (!name) return null;
  const Icon = kind === "config" ? FileCodeIcon : VariableIcon;
  const label = (
    <>
      <Icon className="size-3.5" />
      {name}
    </>
  );
  return docs?.some((d) => d.name === name) ? (
    <Link
      href={docPath(kind, name)}
      className="inline-flex items-center gap-1 text-link hover:underline"
    >
      {label}
    </Link>
  ) : (
    <span className="inline-flex items-center gap-1">{label}</span>
  );
}

export function RunPage({ id }: { id: string }) {
  const { request, url } = useApi();
  const { go, href } = useNavigation();
  const path = `/runs/${id}`;
  const { data: run, error, reload } = usePoll<Run>(path, 1000);
  usePageTitle(run?.name);
  const live = run?.status === "running";
  const { data: viewers } = usePoll<Viewers>(
    run ? `${path}/viewers` : null,
    live ? 1000 : 0,
  );
  const { data: files } = usePoll<RunFile[]>(
    run ? `${path}/files` : null,
    live ? 5000 : 0,
  );
  const { data: yaml } = usePoll<string>(
    run ? `${path}/files/config.yml` : null,
  );
  const { data: inventory } = usePoll<Inventory>(
    run?.deploymentId ? "/systems" : null,
    0,
    true,
  );
  const deployment = inventory?.deployments.find(
    (d) => d.id === run?.deploymentId,
  );
  const room = useMemo(() => {
    if (run?.roomName) return run.roomName;
    const config = parseDocument(yaml ?? "");
    const name = config.getIn(["spec", "conference", "name"]);
    return !config.errors.length && typeof name === "string" ? name : "";
  }, [yaml, run?.roomName]);
  const canJoin =
    deployment?.hostId &&
    ["ready", "unknown"].includes(deployment.status) &&
    inventory?.plugins.find((p) => p.id === deployment.plugin)?.webUI &&
    /^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$/.test(room);
  const now = useNow(live);

  if (!run)
    return (
      <Page className="gap-4">
        <Back href="runs" label="Runs" />
        {error ? (
          <Alert variant="destructive">
            <AlertTitle>{error}</AlertTitle>
            <AlertDescription className="font-mono text-xs">
              {id}
            </AlertDescription>
          </Alert>
        ) : (
          <Spinner />
        )}
      </Page>
    );

  return (
    <Page className="gap-10">
      <div className="flex flex-col gap-4">
        <Back href="runs" label="Runs" />
        <header className="flex flex-wrap items-center gap-x-4 gap-y-3">
          <RunName run={run} path={path} onSaved={reload} />
          <RunStatus status={run.status} />
          <PageActions>
            {canJoin && (
              <Button variant="secondary" asChild>
                <a
                  href={url(
                    `/deployments/${run.deploymentId}/web?room=${encodeURIComponent(room)}`,
                  )}
                  target="_blank"
                  rel="noreferrer"
                >
                  <ExternalLinkIcon />
                  Join room
                </a>
              </Button>
            )}
            {live ? (
              <ConfirmDialog
                trigger={
                  <Button variant="destructive">
                    <CircleStopIcon />
                    Stop
                  </Button>
                }
                title="Stop run"
                name={run.name}
                confirmLabel="Stop"
                onConfirm={async () => {
                  await request("POST", `${path}/stop`);
                  reload();
                }}
              />
            ) : (
              <>
                <NewRun
                  from={run}
                  trigger={
                    <Button variant="secondary">
                      <RotateCcwIcon />
                      Run again
                    </Button>
                  }
                />
                <ConfirmDialog
                  trigger={
                    <Button variant="destructive">
                      <Trash2Icon />
                      Delete
                    </Button>
                  }
                  title="Delete run"
                  name={run.name}
                  confirmLabel="Delete"
                  onConfirm={async () => {
                    await request("DELETE", path);
                    go("/runs");
                  }}
                />
              </>
            )}
          </PageActions>
        </header>
        <div className="flex flex-wrap items-center gap-x-5 gap-y-2 text-sm text-muted-foreground">
          <DocLink kind="config" name={run.configName} />
          <Link
            className="text-link"
            href={
              run.deploymentId
                ? systemPath("deployments", run.deploymentId)
                : undefined
            }
          >
            {run.pluginName}
          </Link>
          {!run.deploymentId && (
            <DocLink kind="environment" name={run.envName} />
          )}
          <span>{time(run.startedAt)}</span>
          <span className="font-mono tabular-nums">
            {duration(elapsed(run, now))}
          </span>
          <span className="inline-flex items-center gap-1 font-mono text-xs">
            {run.id}
            <CopyButton
              value={
                typeof document === "undefined"
                  ? href(runPath(run.id))
                  : new URL(href(runPath(run.id)), document.baseURI).href
              }
              label="Copy link"
            />
          </span>
        </div>
      </div>
      {run.error && (
        <Alert variant="destructive">
          <AlertTitle>Error</AlertTitle>
          <AlertDescription className="font-mono text-xs">
            {run.error}
          </AlertDescription>
        </Alert>
      )}
      {viewers && <ViewersSection data={viewers} live={live} />}
      {run.summary && (
        <SummarySection summary={run.summary} failures={run.failures ?? []} />
      )}
      <LogsSection path={path} files={files} live={live} />
      <FilesSection path={path} files={files} />
      {yaml !== undefined && (
        <section className="flex flex-col gap-4">
          <h2 className="text-lg font-semibold">Config</h2>
          <YamlView text={yaml} />
        </section>
      )}
    </Page>
  );
}
