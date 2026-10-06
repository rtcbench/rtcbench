import { type ComponentType, type ReactNode, useEffect, useState } from "react";
import {
  CopyIcon,
  FileTextIcon,
  PlayIcon,
  PlusIcon,
  SaveIcon,
  Trash2Icon,
  UploadIcon,
} from "lucide-react";
import {
  type Doc,
  type DocKind,
  docApi,
  docPath,
  go,
  pages,
  parseConfig,
  request,
  show,
  time,
  usePoll,
  validName,
} from "./api";
import {
  ConfirmDialog,
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "./dialog";
import { NewRun } from "./runs";
import { column, mono, Table, type TableColumn } from "./table";
import {
  Alert,
  AlertDescription,
  AlertTitle,
  Back,
  Button,
  Field,
  FieldError,
  FieldLabel,
  Input,
  Spinner,
} from "./ui";
import { YamlEditor } from "./yaml";

type Row = Doc & ReturnType<typeof parseConfig>;

const columns: TableColumn<Row>[] = [
  { ...column("name", "Name", (c) => c.name), rowHeader: true },
  mono("plugin", "Plugin", (c) => c.summary.plugin),
  mono("scenario", "Scenario", (c) => c.summary.scenario),
  { ...column("rooms", "Rooms", (c) => show(c.summary.rooms)), numeric: true },
  {
    ...column("users", "Users per room", (c) => show(c.summary.usersPerRoom)),
    numeric: true,
  },
  {
    ...column("cameras", "Cameras per room", (c) =>
      show(c.summary.camerasPerRoom),
    ),
    numeric: true,
  },
  mono("server", "Server", (c) => c.summary.serverIP),
  column(
    "updated",
    "Updated",
    (c) => Date.parse(c.updatedAt),
    (c) => time(c.updatedAt),
  ),
];

type Examples = { pattern: string; files: string[] };

function CopyExamples({
  examples,
  onCopied,
}: {
  examples: Examples;
  onCopied: () => void;
}) {
  return (
    <Dialog>
      <DialogTrigger asChild>
        <Button variant="secondary">
          <CopyIcon />
          Copy from <span className="font-mono">{examples.pattern}</span>
        </Button>
      </DialogTrigger>
      <DialogContent aria-describedby={undefined}>
        <DialogHeader>
          <DialogTitle>
            Copy {examples.files.length} example configs?
          </DialogTitle>
        </DialogHeader>
        <ul className="code-block max-h-72 overflow-auto">
          {examples.files.map((file) => (
            <li key={file}>{file}</li>
          ))}
        </ul>
        <DialogFooter>
          <DialogClose asChild>
            <Button type="button" variant="secondary">
              Cancel
            </Button>
          </DialogClose>
          <Button
            onClick={async () => {
              await request("POST", "/examples");
              onCopied();
            }}
          >
            <CopyIcon />
            Copy
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function ConfigsPage() {
  const { data: configs, reload } = usePoll<Doc[]>("/configs");
  const { data: examples } = usePoll<Examples>(
    configs?.length === 0 ? "/examples" : null,
  );
  const [errors, setErrors] = useState<string[]>([]);

  async function upload(files: File[]) {
    const failed: string[] = [];
    for (const file of files)
      await request("POST", "/configs", {
        name: file.name.replace(/\.ya?ml$/, ""),
        yaml: await file.text(),
      }).catch((err: Error) => failed.push(err.message));
    setErrors(failed);
    reload();
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center gap-3">
        <h1 className="font-display text-2xl font-bold tracking-tight">
          Configs
        </h1>
        <div className="ml-auto flex gap-2">
          <Button variant="secondary" asChild>
            <label>
              <UploadIcon />
              Upload
              <input
                type="file"
                accept=".yml,.yaml"
                multiple
                className="sr-only"
                onChange={(event) => {
                  upload([...(event.target.files ?? [])]);
                  event.target.value = "";
                }}
              />
            </label>
          </Button>
          <Button onClick={() => go("/configs/new")}>
            <PlusIcon />
            New config
          </Button>
        </div>
      </div>
      {errors.length > 0 && (
        <Alert variant="destructive">
          <AlertTitle>Upload failed</AlertTitle>
          <AlertDescription>
            {errors.map((error) => (
              <span key={error}>{error}</span>
            ))}
          </AlertDescription>
        </Alert>
      )}
      <Table
        label="Configs"
        empty={
          examples?.files.length ? (
            <CopyExamples examples={examples} onCopied={reload} />
          ) : (
            "No configs"
          )
        }
        rows={configs?.map((c) => ({ ...c, ...parseConfig(c.yaml) })) ?? []}
        loading={!configs}
        rowKey={(c) => c.name}
        onRowActivate={(c) => go(docPath("config", c.name))}
        rowLabel={(c) => c.name}
        columns={columns}
      />
    </div>
  );
}

export type EditorProps = {
  value: string;
  onChange: (yaml: string, valid: boolean) => void;
};

export function DocumentPage({
  kind,
  name: current,
  actions,
  Editor,
}: {
  kind: DocKind;
  name?: string;
  actions?: (name: string) => ReactNode;
  Editor: ComponentType<EditorProps>;
}) {
  const plural = `${kind}s` as const;
  const label = pages[plural].slice(0, -1);
  const { data: loaded, error: missing } = usePoll<Doc>(
    current ? docApi(kind, current) : null,
  );
  const [doc, setDoc] = useState<Doc>();
  const [name, setName] = useState("");
  const [yaml, setYaml] = useState("");
  const [valid, setValid] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string>();

  useEffect(() => {
    if (!loaded) return;
    setDoc(loaded);
    setName(loaded.name);
    setYaml(loaded.yaml);
  }, [loaded]);

  if (current && !doc)
    return (
      <div className="flex flex-col gap-4">
        <Back href={`/${plural}`} label={pages[plural]} />
        {missing ? (
          <Alert variant="destructive">
            <AlertTitle>{label} not found</AlertTitle>
          </Alert>
        ) : (
          <Spinner />
        )}
      </div>
    );

  const dirty = !doc || name !== doc.name || yaml !== doc.yaml;
  const invalid = name.trim() !== "" && !validName(name.trim());

  async function save() {
    setSaving(true);
    setError(undefined);
    try {
      const saved = await request<Doc>(
        doc ? "PUT" : "POST",
        doc ? docApi(kind, doc.name) : `/${plural}`,
        { name, yaml },
      );
      setDoc(saved);
      setYaml(saved.yaml);
      if (saved.name !== current) go(docPath(kind, saved.name));
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <Back href={`/${plural}`} label={pages[plural]} />
      <div className="flex flex-wrap items-center gap-3">
        <h1 className="font-display text-2xl font-bold tracking-tight">
          {doc?.name ?? `New ${label.toLowerCase()}`}
        </h1>
        <div className="ml-auto flex gap-2">
          {doc && (
            <>
              <Button variant="link" asChild>
                <a
                  href={`${docPath(kind, doc.name)}.yml`}
                  target="_blank"
                  rel="noreferrer"
                >
                  <FileTextIcon />
                  Raw
                </a>
              </Button>
              {actions?.(doc.name)}
              <ConfirmDialog
                trigger={
                  <Button variant="destructive">
                    <Trash2Icon />
                    Delete
                  </Button>
                }
                title={`Delete ${label.toLowerCase()}`}
                name={doc.name}
                confirmLabel="Delete"
                typeToConfirm
                onConfirm={async () => {
                  await request("DELETE", docApi(kind, doc.name));
                  go(`/${plural}`);
                }}
              />
            </>
          )}
          <Button
            onClick={save}
            disabled={!name.trim() || invalid || !valid || !dirty || saving}
          >
            {saving ? <Spinner /> : <SaveIcon />}
            Save
          </Button>
        </div>
      </div>
      <Field data-invalid={error || invalid ? true : undefined}>
        <FieldLabel htmlFor={`${kind}-name`}>Name</FieldLabel>
        <Input
          id={`${kind}-name`}
          aria-invalid={invalid}
          value={name}
          onChange={(event) => setName(event.target.value)}
        />
        {error && <FieldError>{error}</FieldError>}
      </Field>
      <Editor
        key={doc?.updatedAt}
        value={yaml}
        onChange={(next, ok) => {
          setYaml(next);
          setValid(ok);
        }}
      />
    </div>
  );
}

function ConfigEditor({ value, onChange }: EditorProps) {
  const errors = parseConfig(value).errors;
  return (
    <Field data-invalid={errors.length ? true : undefined}>
      <FieldLabel htmlFor="config-yaml">YAML</FieldLabel>
      <YamlEditor
        id="config-yaml"
        className="min-h-[36rem]"
        value={value}
        onChange={(yaml) => onChange(yaml, true)}
      />
      {errors.map((message) => (
        <FieldError key={message}>{message}</FieldError>
      ))}
    </Field>
  );
}

export function ConfigPage({ name }: { name?: string }) {
  return (
    <DocumentPage
      kind="config"
      name={name}
      Editor={ConfigEditor}
      actions={(config) => (
        <NewRun
          configName={config}
          trigger={
            <Button variant="secondary">
              <PlayIcon />
              Run
            </Button>
          }
        />
      )}
    />
  );
}
