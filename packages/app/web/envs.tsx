import { useNavigation, useUnsavedChanges } from "./integration";

import {
  column,
  Table,
  Button,
  cn,
  Field,
  FieldError,
  Input,
  Page,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Spinner,
} from "@rtcbench/admin";
import { Fragment, useId, useState } from "react";
import { PlusIcon, SaveIcon, XIcon } from "lucide-react";
import {
  type Doc,
  docApi,
  docPath,
  type EnvVar,
  formatEnv,
  parseEnv,
  useApi,
  usePoll,
} from "./api";
import { DocumentPage, type EditorProps } from "./configs";

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
  const id = useId();
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
      <span className="text-sm font-medium">Variables</span>
      {entries.length > 0 && (
        <div className="grid grid-cols-[minmax(0,1fr)_auto_minmax(0,2fr)_auto] items-center gap-x-3 gap-y-2">
          {entries.map((e, i) => (
            <Fragment key={i}>
              <Input
                id={`${id}-var-${i}`}
                aria-label={`Variable ${i + 1} name`}
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
                <SelectTrigger
                  aria-label={`${e.name || `Variable ${i + 1}`} type`}
                  className="w-32"
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="variable">Variable</SelectItem>
                  <SelectItem value="secret">Secret</SelectItem>
                  {system && <SelectItem value="system">System</SelectItem>}
                </SelectContent>
              </Select>
              <Input
                aria-label={`${e.name || `Variable ${i + 1}`} value`}
                className={cn(
                  "font-mono",
                  e.type === "secret" && "[-webkit-text-security:disc]",
                )}
                spellCheck="false"
                placeholder={
                  e.type === "variable"
                    ? "Value"
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
                aria-label={`Remove ${e.name || `variable ${i + 1}`}`}
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

export function SystemOverrides({ name }: { name?: string }) {
  const { request } = useApi();
  const defaults = usePoll<Doc>(name ? null : "/system-overrides");
  const documents = usePoll<Doc[]>(name ? "/environments" : null);
  const saved = documents.data?.find((d) => d.name === name);
  const data = name
    ? documents.data
      ? (saved ?? { name, yaml: "[]", updatedAt: "new" })
      : undefined
    : defaults.data;
  const reload = name ? documents.reload : defaults.reload;
  const loadError = name ? documents.error : defaults.error;
  const [yaml, setYaml] = useState<string>();
  const [valid, setValid] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string>();
  useUnsavedChanges(yaml !== undefined);

  async function save() {
    setSaving(true);
    setError(undefined);
    try {
      await request(
        name && !saved ? "POST" : "PUT",
        name
          ? saved
            ? docApi("environment", name)
            : "/environments"
          : "/system-overrides",
        { name, yaml },
      );
      setYaml(undefined);
      await reload();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setSaving(false);
    }
  }

  const saveButton = (
    <Button
      className="ml-auto"
      onClick={save}
      disabled={yaml === undefined || !valid || saving}
    >
      {saving ? <Spinner /> : <SaveIcon />}
      Save
    </Button>
  );

  const editor = (
    <>
      {data ? (
        <EnvEditor
          key={data.updatedAt}
          value={data.yaml}
          system={!!name}
          onChange={(next, ok) => {
            setYaml(next);
            setValid(ok);
          }}
        />
      ) : !loadError ? (
        <Spinner />
      ) : null}
      {(error || loadError) && <FieldError>{error || loadError}</FieldError>}
    </>
  );
  return !name ? (
    <Page title="System overrides" actions={saveButton}>
      {editor}
    </Page>
  ) : (
    <section className="flex flex-col gap-4">
      <div className="flex items-center gap-3">
        <h2 className="text-lg font-semibold">Variables</h2>
        {saveButton}
      </div>
      {editor}
    </section>
  );
}

export function EnvPage({ name }: { name?: string }) {
  return (
    <DocumentPage
      key={name}
      kind="environment"
      name={name}
      Editor={EnvEditor}
    />
  );
}

export function VariableSets() {
  const { open } = useNavigation();
  const { data, error } = usePoll<Doc[]>("/environments", 0, true);
  const rows = data?.filter((d) => !d.name.startsWith("plugins/")) ?? [];
  return (
    <Page
      title="Environments"
      actions={
        <Button onClick={() => open("/configs/variables/new")}>
          <PlusIcon />
          New environment
        </Button>
      }
    >
      {error && <FieldError>{error}</FieldError>}
      <Table
        label="Environments"
        empty="Empty"
        loading={!data && !error}
        rows={rows}
        rowKey={(d) => d.name}
        rowLabel={(d) => d.name}
        onRowActivate={(d) => open(docPath("environment", d.name))}
        columns={[column<Doc>("name", "Name", (d) => d.name)]}
      />
    </Page>
  );
}
