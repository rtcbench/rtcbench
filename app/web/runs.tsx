import { type FormEvent, type ReactElement, useState } from "react";
import { PlayIcon } from "lucide-react";
import {
  type Doc,
  duration,
  elapsed,
  go,
  request,
  type Run,
  runPath,
  type RunStatus as Status,
  time,
  useNow,
  usePoll,
  validName,
} from "./api";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "./dialog";
import { column, Table, type TableColumn } from "./table";
import {
  Alert,
  AlertTitle,
  Badge,
  Button,
  Checkbox,
  Field,
  FieldError,
  FieldLabel,
  Input,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Spinner,
} from "./ui";

const variants = {
  running: "default",
  completed: "success",
  failed: "destructive",
  interrupted: "warning",
} as const;

export function RunStatus({ status }: { status: Status }) {
  return (
    <Badge variant={variants[status]}>
      {status === "running" && <Spinner className="size-3" />}
      {status}
    </Badge>
  );
}

const noEnv = ".";

export function NewRun({
  configName,
  from,
  trigger,
}: {
  configName?: string;
  from?: Run;
  trigger?: ReactElement;
}) {
  const [open, setOpen] = useState(false);
  const { data: configs } = usePoll<Doc[]>(open ? "/configs" : null);
  const { data: envs } = usePoll<Doc[]>(open ? "/environments" : null);
  const [name, setName] = useState(from?.name ?? "");
  const [config, setConfig] = useState(configName ?? from?.configName ?? "");
  const [env, setEnv] = useState(from?.envName ?? noEnv);
  const [startAt, setStartAt] = useState("");
  const [cpuProfile, setCpuProfile] = useState(from?.cpuProfile ?? false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string>();

  const selected = configs?.find((c) => c.name === config);
  const selectedEnv = envs?.find((e) => e.name === env);
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
        envName: selectedEnv?.name,
        startAt: startAt
          ? Math.floor(new Date(startAt).getTime() / 1000)
          : undefined,
        cpuProfile,
      });
      setOpen(false);
      go(runPath(run.id));
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
        <form className="flex flex-col gap-5" onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>New run</DialogTitle>
          </DialogHeader>
          <Field data-invalid={invalid || undefined}>
            <FieldLabel htmlFor="run-name">Name</FieldLabel>
            <Input
              id="run-name"
              placeholder={config}
              aria-invalid={invalid}
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="run-config">Config</FieldLabel>
            <Select value={config} onValueChange={setConfig}>
              <SelectTrigger id="run-config">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {configs?.map((c) => (
                  <SelectItem key={c.name} value={c.name}>
                    {c.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field>
            <FieldLabel htmlFor="run-env">Environment</FieldLabel>
            <Select value={env} onValueChange={setEnv}>
              <SelectTrigger id="run-env">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={noEnv}>System (default)</SelectItem>
                {envs?.map((e) => (
                  <SelectItem key={e.name} value={e.name}>
                    {e.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field>
            <FieldLabel htmlFor="run-start-at">-start-at</FieldLabel>
            <Input
              id="run-start-at"
              type="datetime-local"
              value={startAt}
              onChange={(event) => setStartAt(event.target.value)}
            />
          </Field>
          <Field orientation="horizontal">
            <Checkbox
              id="run-cpuprofile"
              checked={cpuProfile}
              onCheckedChange={(checked) => setCpuProfile(checked === true)}
            />
            <FieldLabel htmlFor="run-cpuprofile">-cpuprofile</FieldLabel>
          </Field>
          {error && <FieldError>{error}</FieldError>}
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="secondary">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={!selected || invalid || pending}>
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
  const { data: runs, error } = usePoll<Run[]>("/runs", 2000);
  const now = useNow(!!runs?.some((r) => r.status === "running"));

  const columns: TableColumn<Run>[] = [
    { ...column("name", "Name", (r) => r.name), rowHeader: true },
    column("config", "Config", (r) => r.configName),
    column("env", "Environment", (r) => r.envName),
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
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between gap-4">
        <h1 className="font-display text-2xl font-bold tracking-tight">Runs</h1>
        <NewRun />
      </div>
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
          onRowActivate={(r) => go(runPath(r.id))}
          rowLabel={(r) => r.name}
          columns={columns}
        />
      )}
    </div>
  );
}
