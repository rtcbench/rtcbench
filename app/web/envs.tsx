import { Fragment, useState } from "react";
import { PlusIcon, SaveIcon, XIcon } from "lucide-react";
import {
  type Doc,
  docPath,
  type EnvVar,
  formatEnv,
  go,
  parseEnv,
  request,
  time,
  usePoll,
} from "./api";
import { DocumentPage, type EditorProps } from "./configs";
import { column, mono, Table, type TableColumn } from "./table";
import {
  Button,
  cn,
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

type Row = Doc & { vars: EnvVar[] };

const columns: TableColumn<Row>[] = [
  { ...column("name", "Name", (e) => e.name), rowHeader: true },
  mono("variables", "Variables", (e) => e.vars.map((v) => v.name).join(" ")),
  column(
    "updated",
    "Updated",
    (e) => Date.parse(e.updatedAt),
    (e) => time(e.updatedAt),
  ),
];

type Entry = EnvVar & { value: string; saved: boolean };

const badName = (entries: Entry[], e: Entry) =>
  !/^[A-Za-z_]\w*$/.test(e.name) ||
  entries.filter((o) => o.name === e.name).length > 1;

const badValue = (e: Entry) => e.type === "secret" && !e.saved && !e.value;

function EnvEditor({
  value,
  onChange,
  system = true,
}: EditorProps & { system?: boolean }) {
  const [entries, setEntries] = useState<Entry[]>(() =>
    parseEnv(value).map((v) => ({ ...v, value: v.value ?? "", saved: true })),
  );

  function update(next: Entry[]) {
    setEntries(next);
    onChange(
      formatEnv(
        next.map(({ name, type, value }) =>
          type === "system" || (type === "secret" && !value)
            ? { name, type }
            : { name, type, value },
        ),
      ),
      !next.some((e) => badName(next, e) || badValue(e)),
    );
  }

  const set = (i: number, change: Partial<Entry>) =>
    update(entries.map((e, j) => (j === i ? { ...e, ...change } : e)));

  return (
    <Field>
      <FieldLabel htmlFor="env-var-0">Variables</FieldLabel>
      {entries.length > 0 && (
        <div className="grid grid-cols-[minmax(0,1fr)_auto_minmax(0,2fr)_auto] items-center gap-x-3 gap-y-2">
          {entries.map((e, i) => (
            <Fragment key={i}>
              <Input
                id={`env-var-${i}`}
                aria-label="Name"
                className="font-mono"
                placeholder="PLUGIN_ID"
                disabled={e.saved}
                aria-invalid={badName(entries, e)}
                value={e.name}
                onChange={(event) => set(i, { name: event.target.value })}
              />
              <Select
                value={e.type}
                disabled={e.saved}
                onValueChange={(type) =>
                  set(i, { type: type as Entry["type"] })
                }
              >
                <SelectTrigger aria-label="Type" className="w-32">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="variable">Variable</SelectItem>
                  <SelectItem value="secret">Secret</SelectItem>
                  {system && <SelectItem value="system">System</SelectItem>}
                </SelectContent>
              </Select>
              <Input
                aria-label="Value"
                className={cn(
                  "font-mono",
                  e.type === "secret" && "[-webkit-text-security:disc]",
                )}
                spellCheck="false"
                placeholder={
                  e.type === "variable"
                    ? "livekit"
                    : e.type === "secret" && e.saved
                      ? "••••••••"
                      : undefined
                }
                disabled={e.type === "system"}
                aria-invalid={badValue(e)}
                value={e.type === "system" ? "" : e.value}
                onChange={(event) => set(i, { value: event.target.value })}
              />
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="Remove"
                onClick={() => update(entries.filter((_, j) => j !== i))}
              >
                <XIcon />
              </Button>
            </Fragment>
          ))}
        </div>
      )}
      <Button
        variant="secondary"
        className="self-start"
        onClick={() =>
          update([
            ...entries,
            { name: "", type: "variable", value: "", saved: false },
          ])
        }
      >
        <PlusIcon />
        Add variable
      </Button>
    </Field>
  );
}

function SystemOverrides() {
  const { data, reload } = usePoll<Doc>("/system-overrides");
  const [yaml, setYaml] = useState<string>();
  const [valid, setValid] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string>();

  async function save() {
    setSaving(true);
    setError(undefined);
    try {
      await request("PUT", "/system-overrides", { yaml });
      setYaml(undefined);
      await reload();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setSaving(false);
    }
  }

  return (
    <section className="flex flex-col gap-4">
      <div className="flex items-center gap-3">
        <h2 className="text-lg font-semibold">System overrides</h2>
        <Button
          className="ml-auto"
          onClick={save}
          disabled={yaml === undefined || !valid || saving}
        >
          {saving ? <Spinner /> : <SaveIcon />}
          Save
        </Button>
      </div>
      {data ? (
        <EnvEditor
          key={data.updatedAt}
          value={data.yaml}
          system={false}
          onChange={(next, ok) => {
            setYaml(next);
            setValid(ok);
          }}
        />
      ) : (
        <Spinner />
      )}
      {error && <FieldError>{error}</FieldError>}
    </section>
  );
}

export function EnvsPage() {
  const { data: envs } = usePoll<Doc[]>("/environments");
  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center gap-3">
        <h1 className="font-display text-2xl font-bold tracking-tight">
          Environments
        </h1>
        <Button className="ml-auto" onClick={() => go("/environments/new")}>
          <PlusIcon />
          New environment
        </Button>
      </div>
      <Table
        label="Environments"
        empty="No environments"
        rows={envs?.map((e) => ({ ...e, vars: parseEnv(e.yaml) })) ?? []}
        loading={!envs}
        rowKey={(e) => e.name}
        onRowActivate={(e) => go(docPath("environment", e.name))}
        rowLabel={(e) => e.name}
        columns={columns}
      />
      <div className="mt-6">
        <SystemOverrides />
      </div>
    </div>
  );
}

export function EnvPage({ name }: { name?: string }) {
  return <DocumentPage kind="environment" name={name} Editor={EnvEditor} />;
}
