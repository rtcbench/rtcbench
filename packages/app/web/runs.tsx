import { useNavigation } from "./integration";

import type { Inventory } from "./systems/model";
import {
  SelectField,
  TextField,
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
  column,
  Table,
  type TableColumn,
  Alert,
  AlertTitle,
  Badge,
  Button,
  Checkbox,
  Field,
  FieldError,
  FieldLabel,
  Page,
  Spinner,
} from "@labpane/ui";
import { type FormEvent, type ReactElement, useId, useState } from "react";
import { PlayIcon } from "lucide-react";
import {
  type Doc,
  duration,
  elapsed,
  useApi,
  type Run,
  runPath,
  type RunStatus as Status,
  time,
  useNow,
  usePoll,
  validName,
} from "./api";

const variants = {
  running: "default",
  completed: "success",
  failed: "destructive",
  interrupted: "warning",
} as const;

export function RunStatus({ status }: { status: Status }) {
  return (
    <Badge variant={variants[status]}>
      {status === "running" && <Spinner className="labui:size-3" />}
      {status}
    </Badge>
  );
}

const noEnv = ".";

export function NewRun({
  configName,
  deploymentId,
  from,
  trigger,
}: {
  configName?: string;
  deploymentId?: string;
  from?: Run;
  trigger?: ReactElement;
}) {
  const { request } = useApi();
  const { open: openTab } = useNavigation();
  const id = useId();
  const [open, setOpen] = useState(false);
  const { data: configs } = usePoll<Doc[]>(open ? "/configs" : null);
  const { data: envs } = usePoll<Doc[]>(open ? "/environments" : null);
  const [name, setName] = useState(from?.name ?? "");
  const [config, setConfig] = useState(configName ?? from?.configName ?? "");
  const { data: inventory } = usePoll<Inventory>(
    open ? "/systems" : null,
    2500,
  );
  const [destination, setDestination] = useState(
    deploymentId ?? from?.deploymentId ?? noEnv,
  );
  const [environment, setEnvironment] = useState(from?.envName ?? "");
  const [startAt, setStartAt] = useState("");
  const [cpuProfile, setCpuProfile] = useState(from?.cpuProfile ?? false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string>();

  const selected = configs?.find((c) => c.name === config);
  const profiles = inventory?.deployments ?? [];
  const profile = profiles.find((p) => p.id === destination);
  const invalid = name.trim() !== "" && !validName(name.trim());

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!selected) return;
    setPending(true);
    setError(undefined);
    try {
      const run = await request<Run>("POST", "/runs", {
        name: name.trim(),
        configName: selected.name,
        config: selected.yaml,
        envName: destination === noEnv ? environment || undefined : undefined,
        deploymentId: profile?.id,
        startAt: startAt
          ? Math.floor(new Date(startAt).getTime() / 1000)
          : undefined,
        cpuProfile,
      });
      setOpen(false);
      openTab(runPath(run.id));
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setPending(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        {trigger ?? (
          <Button>
            <PlayIcon />
            New run
          </Button>
        )}
      </DialogTrigger>
      <DialogContent aria-describedby={undefined}>
        <form
          className="labui:flex labui:flex-col labui:gap-5"
          onSubmit={submit}
        >
          <DialogHeader>
            <DialogTitle>New run</DialogTitle>
          </DialogHeader>
          <TextField
            label="Name"
            placeholder={config}
            aria-invalid={invalid}
            value={name}
            onChange={setName}
          />
          <SelectField
            label="Config"
            value={config}
            onChange={setConfig}
            options={(configs ?? []).map((c) => ({ id: c.name, name: c.name }))}
          />
          <SelectField
            label="Deployment"
            value={destination}
            onChange={setDestination}
            options={[
              { id: noEnv, name: "From config" },
              ...profiles.map((p) => ({
                id: p.id,
                name: p.name,
                disabled:
                  !["ready", "configured"].includes(p.status) || !!p.operation,
              })),
            ]}
          />
          {destination === noEnv &&
            !!envs?.filter((e) => !e.name.startsWith("plugins/")).length && (
              <SelectField
                label="Variables"
                value={environment || "."}
                onChange={(v) => setEnvironment(v === "." ? "" : v)}
                options={[
                  { id: ".", name: "None" },
                  ...envs
                    .filter((e) => !e.name.startsWith("plugins/"))
                    .map((e) => ({ id: e.name, name: e.name })),
                ]}
              />
            )}
          <TextField
            label="Start at"
            type="datetime-local"
            value={startAt}
            onChange={setStartAt}
          />
          <Field orientation="horizontal">
            <Checkbox
              id={`${id}-cpuprofile`}
              checked={cpuProfile}
              onCheckedChange={(checked) => setCpuProfile(checked === true)}
            />
            <FieldLabel htmlFor={`${id}-cpuprofile`}>
              Record CPU profile
            </FieldLabel>
          </Field>
          {error && <FieldError>{error}</FieldError>}
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="secondary">
                Cancel
              </Button>
            </DialogClose>
            <Button
              type="submit"
              disabled={
                !selected ||
                invalid ||
                pending ||
                (destination !== noEnv &&
                  (!profile ||
                    !["ready", "configured"].includes(profile.status) ||
                    !!profile.operation))
              }
            >
              {pending ? <Spinner /> : <PlayIcon />}
              Start
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export function RunsPage() {
  const { open: openTab } = useNavigation();
  const { data: runs, error } = usePoll<Run[]>("/runs", 2000);
  const now = useNow(!!runs?.some((r) => r.status === "running"));

  const columns: TableColumn<Run>[] = [
    { ...column("name", "Name", (r) => r.name), rowHeader: true },
    column("config", "Config", (r) => r.configName),
    column("server", "Deployment", (r) => r.pluginName || "From config"),
    column(
      "status",
      "Status",
      (r) => r.status,
      (r) => <RunStatus status={r.status} />,
    ),
    column(
      "started",
      "Started",
      (r) => Date.parse(r.startedAt),
      (r) => time(r.startedAt),
    ),
    {
      ...column(
        "duration",
        "Duration",
        (r) => elapsed(r, now),
        (r) => duration(elapsed(r, now)),
      ),
      numeric: true,
    },
  ];

  return (
    <Page title="Runs" actions={<NewRun />}>
      {error && !runs ? (
        <Alert variant="destructive">
          <AlertTitle>{error}</AlertTitle>
        </Alert>
      ) : (
        <Table
          label="Runs"
          empty="No runs"
          rows={runs ?? []}
          loading={!runs}
          rowKey={(r) => r.id}
          onRowActivate={(r) => openTab(runPath(r.id))}
          rowLabel={(r) => r.name}
          columns={columns}
        />
      )}
    </Page>
  );
}
