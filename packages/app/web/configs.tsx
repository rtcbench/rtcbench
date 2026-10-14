import {
  useNavigation,
  useUnsavedChanges,
  usePageTitle,
  Back,
} from "./integration";

import {
  type ComponentType,
  type ReactNode,
  useEffect,
  useId,
  useState,
} from "react";
import {
  CopyIcon,
  PencilIcon,
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
  parseConfig,
  useApi,
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
  column,
  mono,
  Table,
  type TableColumn,
  Alert,
  AlertDescription,
  AlertTitle,
  Button,
  Field,
  FieldError,
  FieldLabel,
  Input,
  Page,
  PageHeader,
  Spinner,
} from "@labpane/ui";
import { NewRun } from "./runs";

import { YamlEditor, YamlView } from "./yaml";

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
  const { request } = useApi();
  return (
    <Dialog>
      <DialogTrigger asChild>
        <Button variant="secondary">
          <CopyIcon />
          Copy from <span className="labui:font-mono">{examples.pattern}</span>
        </Button>
      </DialogTrigger>
      <DialogContent aria-describedby={undefined}>
        <DialogHeader>
          <DialogTitle>
            Copy {examples.files.length} example configs?
          </DialogTitle>
        </DialogHeader>
        <ul className="rtc-code-block labui:max-h-72 labui:overflow-auto">
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
  const { request } = useApi();
  const { open } = useNavigation();
  const {
    data: configs,
    reload,
    error: loadError,
  } = usePoll<Doc[]>("/configs", 0, true);
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
    <Page
      title="Configs"
      actions={
        <>
          <Button variant="secondary" asChild>
            <label>
              <UploadIcon />
              Upload
              <input
                type="file"
                accept=".yml,.yaml"
                multiple
                className="labui:sr-only"
                onChange={(event) => {
                  upload([...(event.target.files ?? [])]);
                  event.target.value = "";
                }}
              />
            </label>
          </Button>
          <Button onClick={() => open("/configs/new")}>
            <PlusIcon />
            New config
          </Button>
        </>
      }
    >
      {loadError && <FieldError>{loadError}</FieldError>}
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
        loading={!configs && !loadError}
        rowKey={(c) => c.name}
        onRowActivate={(c) => open(docPath("config", c.name))}
        rowLabel={(c) => c.name}
        columns={columns}
      />
    </Page>
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
  const { request } = useApi();
  const { go } = useNavigation();
  const nameId = useId();
  const plural = `${kind}s` as const;
  const label = kind === "environment" ? "Environment" : "Config";
  const back = kind === "environment" ? "configs/variables" : "configs";
  const backLabel = kind === "environment" ? "Environments" : "Configs";
  const { data: loaded, error: missing } = usePoll<Doc>(
    current ? docApi(kind, current) : null,
  );
  const [doc, setDoc] = useState<Doc>();
  const [name, setName] = useState("");
  const [yaml, setYaml] = useState("");
  const [valid, setValid] = useState(true);
  const [saving, setSaving] = useState(false);
  const [editing, setEditing] = useState(!current);
  const [error, setError] = useState<string>();
  useUnsavedChanges(
    editing &&
      (doc ? name !== doc.name || yaml !== doc.yaml : !!name || !!yaml),
  );
  usePageTitle(doc?.name ?? current);

  useEffect(() => {
    if (!loaded) return;
    setDoc(loaded);
    setName(loaded.name);
    setYaml(loaded.yaml);
  }, [loaded]);

  if (current && !doc)
    return (
      <Page>
        <Back href={back} label={backLabel} />
        {missing ? (
          <Alert variant="destructive">
            <AlertTitle>{label} not found</AlertTitle>
          </Alert>
        ) : (
          <Spinner />
        )}
      </Page>
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
      setName(saved.name);
      setYaml(saved.yaml);
      setEditing(false);
      if (saved.name !== current) go(docPath(kind, saved.name));
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setSaving(false);
    }
  }

  return (
    <Page>
      <Back href={back} label={backLabel} />
      <PageHeader
        title={doc?.name ?? `New ${label.toLowerCase()}`}
        actions={
          <>
            {doc && (
              <>
                <Button variant="link" asChild>
                  <a
                    href={`data:application/yaml;charset=utf-8,${encodeURIComponent(doc.yaml)}`}
                    download={`${doc.name.split("/").at(-1)}.yml`}
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
                    go(`/${back}`);
                  }}
                />
              </>
            )}
            {editing ? (
              <>
                <Button
                  variant="secondary"
                  disabled={saving || !doc}
                  onClick={() => {
                    setName(doc!.name);
                    setYaml(doc!.yaml);
                    setValid(true);
                    setEditing(false);
                    setError(undefined);
                  }}
                >
                  Cancel
                </Button>
                <Button
                  onClick={save}
                  disabled={
                    !name.trim() || invalid || !valid || !dirty || saving
                  }
                >
                  {saving ? <Spinner /> : <SaveIcon />}
                  Save
                </Button>
              </>
            ) : (
              <Button onClick={() => setEditing(true)}>
                <PencilIcon />
                Edit
              </Button>
            )}
          </>
        }
      />
      {editing ? (
        <>
          <Field data-invalid={error || invalid ? true : undefined}>
            <FieldLabel htmlFor={nameId}>Name</FieldLabel>
            <Input
              id={nameId}
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
        </>
      ) : (
        <YamlView text={doc?.yaml ?? ""} />
      )}
    </Page>
  );
}

function ConfigEditor({ value, onChange }: EditorProps) {
  const id = useId();
  const errors = parseConfig(value).errors;
  return (
    <Field data-invalid={errors.length ? true : undefined}>
      <FieldLabel htmlFor={id}>YAML</FieldLabel>
      <YamlEditor
        id={id}
        invalid={errors.length > 0}
        describedBy={errors.length ? `${id}-errors` : undefined}
        className="labui:min-h-[36rem]"
        value={value}
        onChange={(yaml) => onChange(yaml, true)}
      />
      {!!errors.length && (
        <FieldError id={`${id}-errors`}>{errors.join(". ")}</FieldError>
      )}
    </Field>
  );
}

export function ConfigPage({ name }: { name?: string }) {
  return (
    <DocumentPage
      key={name}
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
