import { useId, useRef, useState, type FormEvent } from "react";
import {
  PlayIcon,
  CircleStopIcon,
  DownloadIcon,
  Trash2Icon,
} from "lucide-react";
import {
  Alert,
  AlertTitle,
  Badge,
  Button,
  Checkbox,
  ConfirmDialog,
  Field,
  FieldError,
  FieldLabel,
  Page,
  SelectField,
  Spinner,
  Table,
  TextField,
  column,
} from "@labpane/ui";
import {
  Back,
  useNavigation,
  usePageTitle,
  useUnsavedChanges,
} from "./integration";
import {
  useApi,
  usePoll,
  time,
  validName,
  type RunStatus as Status,
} from "./api";
import { RunStatus } from "./runs";
import { YamlEditor, YamlView } from "./yaml";

type Cell = { sfu: string; client: string };
type BenchmarkRequest = {
  name: string;
  cluster: string;
  mode: string;
  cells: Cell[];
  bounds: { min: number; max: number; hint?: number };
  repetitions: number;
  provision: boolean;
  uploadAssets: boolean;
  dryRun: boolean;
  hints?: Record<string, number>;
};
type Trial = Cell & {
  load: number;
  repeat: number;
  passed: boolean;
  reason: string;
  error?: string;
  samples: {
    host: string;
    quality: { fraction: number; expected: number; healthy: number };
  }[];
  topology: {
    rooms: number;
    totalSenders: number;
    totalReceivers: number;
    expectedStreams: number;
  };
};
type Benchmark = {
  id: string;
  name: string;
  status: Status;
  startedAt: string;
  endedAt?: string;
  error?: string;
  request: BenchmarkRequest;
  report: {
    phase: string;
    results: Record<
      string,
      {
        best: number;
        complete: boolean;
        ceilingReached: boolean;
        limit: number;
        limitReason: string;
      }
    >;
    trials: Trial[];
  };
};

const clusterExample = `ssh_key: ~/.ssh/id_ed25519
ssh_user: root
sfu: 192.0.2.10
sender: 192.0.2.11
receivers: [192.0.2.12]
senders: [192.0.2.12]
viewer: 192.0.2.11
cores: 0-7
warmup_seconds: 90
steady_state_seconds: 120
sample_seconds: 5
min_bitrate_bps: 2800000
min_fps: 20
healthy_fraction: 1
rtcbench_image: rtcbench:latest
webrtcperf_image: ghcr.io/vpalmisano/webrtcperf:latest
remote_ivf_dir: /opt/ivf-videos
video_path: /opt/ivf-videos/1080p30.mp4
`;

function Check({
  label,
  checked,
  change,
}: {
  label: string;
  checked: boolean;
  change: (value: boolean) => void;
}) {
  const id = useId();
  return (
    <Field orientation="horizontal">
      <Checkbox
        id={id}
        checked={checked}
        onCheckedChange={(v) => change(v === true)}
      />
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
    </Field>
  );
}

function NewBenchmark({
  initial,
  onCreated,
}: {
  initial?: BenchmarkRequest;
  onCreated?: () => void;
}) {
  const { request } = useApi();
  const { go } = useNavigation();
  const id = useId();
  const [name, setName] = useState(initial?.name ?? "Capacity test");
  const [cluster, setCluster] = useState(initial?.cluster ?? clusterExample);
  const [mode, setMode] = useState(initial?.mode ?? "receiver");
  const [sfus, setSFUs] = useState(
    initial ? [...new Set(initial.cells.map((c) => c.sfu))] : ["livekit"],
  );
  const [clients, setClients] = useState(
    initial
      ? [...new Set(initial.cells.map((c) => c.client))]
      : ["rtcbench", "chromium"],
  );
  const [min, setMin] = useState(String(initial?.bounds.min ?? 1));
  const [max, setMax] = useState(String(initial?.bounds.max ?? 128));
  const [repetitions, setRepetitions] = useState(
    String(initial?.repetitions ?? 2),
  );
  const [provision, setProvision] = useState(initial?.provision ?? false);
  const [uploadAssets, setUploadAssets] = useState(
    initial?.uploadAssets ?? false,
  );
  const [dryRun, setDryRun] = useState(initial?.dryRun ?? false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string>();
  const [selectionEdited, setSelectionEdited] = useState(false);
  const formValue = JSON.stringify({
    name,
    cluster,
    mode,
    sfus,
    clients,
    min,
    max,
    repetitions,
    provision,
    uploadAssets,
    dryRun,
  });
  const original = useRef(formValue);
  useUnsavedChanges(!pending && original.current !== formValue);
  const toggle = (items: string[], value: string, checked: boolean) =>
    checked ? [...items, value] : items.filter((v) => v !== value);
  async function submit(event: FormEvent) {
    event.preventDefault();
    setPending(true);
    setError(undefined);
    try {
      const next = await request<Benchmark>("POST", "/benchmarks", {
        name: name.trim(),
        cluster,
        mode,
        cells:
          initial && !selectionEdited
            ? initial.cells
            : sfus.flatMap((sfu) => clients.map((client) => ({ sfu, client }))),
        bounds: { min: Number(min), max: Number(max) },
        repetitions: Number(repetitions),
        provision,
        uploadAssets: provision && uploadAssets,
        dryRun,
        hints: initial?.hints,
      });
      original.current = formValue;
      onCreated?.();
      go(`benchmarks/${next.id}`);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setPending(false);
    }
  }
  return (
    <Page title="New benchmark" className="labui:gap-5">
      <Back href="benchmarks" label="Benchmarks" />
      <form
        onSubmit={submit}
        className="labui:flex labui:max-w-4xl labui:flex-col labui:gap-5"
      >
        <TextField label="Name" value={name} onChange={setName} required />
        <SelectField
          label="Mode"
          value={mode}
          onChange={setMode}
          options={[
            { id: "receiver", name: "Receiver capacity" },
            { id: "sender", name: "Sender capacity" },
          ]}
        />
        <p className="labui:text-sm labui:text-muted-foreground">
          {mode === "receiver"
            ? "One publisher per room, with receivers distributed over the load machines. Rooms hold up to 20 receivers per machine."
            : "Senders run on the load machines, with one observer per room on a separate machine. Room limits follow each SFU’s benchmark configuration."}
        </p>
        <fieldset className="labui:flex labui:flex-wrap labui:gap-4">
          <legend className="labui:mb-2 labui:font-medium">SFUs</legend>
          {["janus", "jitsi", "livekit", "mediasoup"].map((sfu) => (
            <Check
              key={sfu}
              label={sfu}
              checked={sfus.includes(sfu)}
              change={(v) => {
                setSelectionEdited(true);
                setSFUs(toggle(sfus, sfu, v));
              }}
            />
          ))}
        </fieldset>
        <fieldset className="labui:flex labui:flex-wrap labui:gap-4">
          <legend className="labui:mb-2 labui:font-medium">Clients</legend>
          {["rtcbench", "chromium", "webrtcperf"].map((client) => (
            <Check
              key={client}
              label={client}
              checked={clients.includes(client)}
              change={(v) => {
                setSelectionEdited(true);
                setClients(toggle(clients, client, v));
              }}
            />
          ))}
        </fieldset>
        <p className="labui:text-sm labui:text-muted-foreground">
          Chromium decodes received video. WebRtcPerf runs with video decoding
          disabled; its receiver check uses bitrate and stream coverage.
        </p>
        <div className="labui:grid labui:gap-4 labui:sm:grid-cols-3">
          <TextField
            label="Minimum per machine"
            type="number"
            min={1}
            value={min}
            onChange={setMin}
            required
          />
          <TextField
            label="Maximum per machine"
            type="number"
            min={1}
            max={1000000}
            value={max}
            onChange={setMax}
            required
          />
          <TextField
            label="Repetitions per load"
            type="number"
            min={1}
            max={10}
            value={repetitions}
            onChange={setRepetitions}
            required
          />
        </div>
        <Field>
          <FieldLabel htmlFor={`${id}-cluster`}>Cluster YAML</FieldLabel>
          <YamlEditor
            id={`${id}-cluster`}
            value={cluster}
            onChange={setCluster}
          />
        </Field>
        <p className="labui:text-sm labui:text-muted-foreground">
          SSH keys and local asset paths are read on the RTCBench server. Load
          hosts need Linux, Docker, the selected images, and matching test
          media. CPU limits apply to load containers; keep the
          publisher/observer and SFU on separate machines. Use image digests for
          reproducible comparisons.
        </p>
        <Check
          label="Start and stop test SFUs"
          checked={provision}
          change={setProvision}
        />
        {provision && (
          <>
            <p className="labui:text-sm labui:text-muted-foreground">
              Starts and stops test SFUs over SSH on the configured hosts.
              Leave this off to use SFUs you have already started.
            </p>
            <Check
              label="Upload prepared binaries and media"
              checked={uploadAssets}
              change={setUploadAssets}
            />
          </>
        )}
        <Check
          label="Simulate search without contacting machines"
          checked={dryRun}
          change={setDryRun}
        />
        {error && <FieldError>{error}</FieldError>}
        <Button
          type="submit"
          disabled={
            pending ||
            !validName(name.trim()) ||
            !sfus.length ||
            !clients.length
          }
          className="labui:self-start"
        >
          {pending ? <Spinner /> : <PlayIcon />}
          {dryRun ? "Simulate" : "Start benchmark"}
        </Button>
      </form>
    </Page>
  );
}

export function BenchmarksPage() {
  const { open } = useNavigation();
  const { data, error } = usePoll<Benchmark[]>("/benchmarks", 2000);
  return (
    <Page
      title="Benchmarks"
      actions={
        <Button onClick={() => open("benchmarks/new")}>
          <PlayIcon />
          New benchmark
        </Button>
      }
    >
      {error && (
        <Alert variant="destructive">
          <AlertTitle>{error}</AlertTitle>
        </Alert>
      )}
      <Table
        label="Benchmarks"
        rows={data ?? []}
        loading={!data && !error}
        empty="No benchmarks"
        rowKey={(b) => b.id}
        rowLabel={(b) => b.name}
        onRowActivate={(b) => open(`benchmarks/${b.id}`)}
        columns={[
          {
            ...column<Benchmark>("name", "Name", (b) => b.name),
            rowHeader: true,
          },
          column<Benchmark>("mode", "Mode", (b) =>
            b.request.dryRun ? "Simulation" : `${b.request.mode} capacity`,
          ),
          column<Benchmark>(
            "status",
            "Status",
            (b) => b.status,
            (b) => <RunStatus status={b.status} />,
          ),
          column<Benchmark>("phase", "Progress", (b) => b.report.phase),
          column<Benchmark>(
            "started",
            "Started",
            (b) => Date.parse(b.startedAt),
            (b) => time(b.startedAt),
          ),
        ]}
      />
    </Page>
  );
}

export function BenchmarkPage({ id }: { id: string }) {
  const { request, url } = useApi();
  const { go } = useNavigation();
  const path = `/benchmarks/${id}`;
  const { data, error, reload } = usePoll<Benchmark>(
    id === "new" ? null : path,
    1500,
  );
  const [actionError, setActionError] = useState<string>();
  const [again, setAgain] = useState(false);
  usePageTitle(id === "new" ? "New benchmark" : data?.name);
  if (id === "new" || again)
    return (
      <NewBenchmark
        initial={again ? data?.request : undefined}
        onCreated={() => setAgain(false)}
      />
    );
  if (!data)
    return (
      <Page>
        <Back href="benchmarks" label="Benchmarks" />
        {error ? <FieldError>{error}</FieldError> : <Spinner />}
      </Page>
    );
  const live = data.status === "running";
  const results = Object.entries(data.report.results ?? {})
    .filter(([, value]) => value.complete)
    .map(([cell, value]) => ({ cell, ...value }));
  const trials = (data.report.trials ?? []).map((trial, index) => ({
    ...trial,
    index,
  }));
  const largest = Math.max(1, ...results.map((r) => r.best));
  const action = async (method: string, suffix = "") => {
    setActionError(undefined);
    try {
      await request(method, path + suffix);
      if (method === "DELETE") go("benchmarks");
      else reload();
    } catch (err) {
      setActionError((err as Error).message);
    }
  };
  return (
    <Page
      title={data.name}
      className="labui:gap-5"
      actions={
        <>
          {live ? (
            <Button
              variant="destructive"
              onClick={() => void action("POST", "/stop")}
            >
              <CircleStopIcon />
              Stop
            </Button>
          ) : (
            <>
              <Button onClick={() => setAgain(true)}>
                <PlayIcon />
                Run again
              </Button>
              <ConfirmDialog
                trigger={
                  <Button variant="ghost">
                    <Trash2Icon />
                    Delete
                  </Button>
                }
                title="Delete benchmark?"
                description="This removes its saved trials and results."
                confirmLabel="Delete"
                onConfirm={() => action("DELETE")}
              />
            </>
          )}
        </>
      }
    >
      <Back href="benchmarks" label="Benchmarks" />
      <div className="labui:flex labui:flex-wrap labui:items-center labui:gap-3">
        <RunStatus status={data.status} />
        {data.request.dryRun && <Badge>Simulation — no measurements</Badge>}
        <span>{data.report.phase}</span>
      </div>
      {(data.error || actionError) && (
        <Alert variant="destructive">
          <AlertTitle>{actionError ?? data.error}</AlertTitle>
        </Alert>
      )}
      <p className="labui:text-sm labui:text-muted-foreground">
        {data.request.mode === "receiver" ? "Receivers" : "Senders"} per load
        machine. Every measured sample must meet the configured bitrate, FPS,
        and coverage rules in every repetition. These checks do not establish
        perceptual quality or full-resolution delivery.
      </p>
      {results.length > 0 && (
        <div
          className="labui:flex labui:max-w-3xl labui:flex-col labui:gap-3"
          role="img"
          aria-label="Highest passing load by client"
        >
          {results.map((r) => (
            <div
              key={r.cell}
              className="labui:grid labui:grid-cols-[10rem_1fr_5rem] labui:items-center labui:gap-3"
            >
              <span>{r.cell}</span>
              <div className="labui:h-5 labui:rounded labui:bg-muted">
                <div
                  className="labui:h-full labui:rounded labui:bg-primary"
                  style={{ width: `${(100 * r.best) / largest}%` }}
                />
              </div>
              <strong title={`Limit ${r.limit}: ${r.limitReason}`}>
                {r.ceilingReached ? "≥ " : ""}
                {r.best}
              </strong>
            </div>
          ))}
          {results.some((r) => r.ceilingReached) && (
            <p className="labui:text-sm labui:text-muted-foreground">
              ≥ means the configured upper bound passed; saturation was not
              reached.
            </p>
          )}
        </div>
      )}
      <div className="labui:flex labui:flex-wrap labui:gap-3">
        {["experiments.jsonl", "benchmark.json", "benchmark.log"].map(
          (file) => (
            <Button key={file} variant="secondary" asChild>
              <a href={url(`${path}/files/${file}`)} download={file}>
                <DownloadIcon />
                {file}
              </a>
            </Button>
          ),
        )}
      </div>
      <BenchmarkFiles path={path} />
      <Table
        label="Trials"
        rows={trials}
        rowKey={(t) => String(t.index)}
        columns={[
          column<Trial>(
            "client",
            "SFU / client",
            (t) => `${t.sfu} / ${t.client}`,
          ),
          {
            ...column<Trial>("load", "Per machine", (t) => t.load),
            numeric: true,
          },
          {
            ...column<Trial>("repeat", "Repeat", (t) => t.repeat),
            numeric: true,
          },
          {
            ...column<Trial>("rooms", "Rooms", (t) => t.topology.rooms),
            numeric: true,
          },
          {
            ...column<Trial>(
              "streams",
              "Expected streams",
              (t) => t.topology.expectedStreams,
            ),
            numeric: true,
          },
          column<Trial>("healthy", "Worst healthy", (t) =>
            t.samples?.length
              ? `${(100 * Math.min(...t.samples.map((s) => s.quality.fraction))).toFixed(1)}%`
              : "—",
          ),
          column<Trial>(
            "result",
            "Result",
            (t) =>
              t.reason === "SIMULATED"
                ? "Simulated"
                : t.passed
                  ? "Pass"
                  : "Fail",
            (t) => (
              <Badge variant={t.passed ? "success" : "destructive"}>
                {t.reason === "SIMULATED"
                  ? "Simulated"
                  : t.passed
                    ? "Pass"
                    : t.reason}
              </Badge>
            ),
          ),
        ]}
      />
      <details>
        <summary className="labui:cursor-pointer labui:font-medium">
          Cluster and quality rules
        </summary>
        <YamlView text={data.request.cluster} />
      </details>
    </Page>
  );
}

function BenchmarkFiles({ path }: { path: string }) {
  const { url } = useApi();
  const { data, error } = usePoll<{ path: string; size: number }[]>(
    `${path}/files`,
    3000,
  );
  return (
    <details>
      <summary className="labui:cursor-pointer labui:font-medium">
        All artifacts
      </summary>
      {error && <FieldError>{error}</FieldError>}
      <ul className="labui:mt-3 labui:space-y-2 labui:text-sm">
        {data?.map((file) => (
          <li key={file.path}>
            <a
              className="labui:underline"
              href={url(
                `${path}/files/${file.path.split("/").map(encodeURIComponent).join("/")}`,
              )}
              download
            >
              {file.path}
            </a>{" "}
            ({file.size.toLocaleString()} bytes)
          </li>
        ))}
      </ul>
    </details>
  );
}
