import { useId, useState } from "react";
import {
  CodeIcon,
  DownloadIcon,
  ClipboardIcon,
  PlusIcon,
  TerminalIcon,
} from "lucide-react";
import {
  Button,
  Field,
  FieldError,
  FieldLabel,
  Input,
  Page,
  Table,
  column,
  type TableColumn,
} from "@labpane/ui";
import { useApi, usePoll } from "./api";
import { Back, Link, useNavigation } from "./integration";
import { pluginPrompt } from "./plugin-prompt";

type Plugin = {
  id: string;
  name: string;
  description: string;
  project?: string;
  error?: string;
};
type Project = { name: string; path: string };
const columns: TableColumn<Plugin>[] = [
  {
    ...column(
      "name",
      "Name",
      (p) => p.name,
      (p) =>
        p.project ? (
          <Link
            className="labui:text-link labui:underline-offset-4 labui:hover:underline"
            href={`plugins/${p.project}`}
          >
            {p.name}
          </Link>
        ) : (
          p.name
        ),
    ),
    rowHeader: true,
  },
  column("id", "Plugin ID", (p) => p.id),
  column("description", "Description", (p) => p.error || p.description),
  column("source", "Source", (p) =>
    p.project ? `plugins/${p.project}/` : "Built-in",
  ),
];

export function PluginsPage() {
  const { data, error } = usePoll<Plugin[]>("/plugins", 0, true);
  const { open } = useNavigation();
  return (
    <Page
      title="Plugins"
      actions={
        <Button onClick={() => open("plugins/new")}>
          <PlusIcon />
          New Plugin
        </Button>
      }
    >
      {error && <FieldError>{error}</FieldError>}
      <Table
        label="Plugins"
        columns={columns}
        rows={data ?? []}
        loading={!data && !error}
        rowKey={(p) =>
          `${p.project ? "project" : "builtin"}:${p.project || p.id}`
        }
      />
    </Page>
  );
}

export function NewPluginPage({ name }: { name?: string }) {
  const { request, url } = useApi();
  const { go, open } = useNavigation();
  const id = useId();
  const endpoint = `/plugins/${encodeURIComponent(name ?? "")}`;
  const { data, error: loadError } = usePoll<Project>(name ? endpoint : null);
  const [projectName, setProjectName] = useState("my-plugin");
  const [module, setModule] = useState("example.com/my-plugin");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [status, setStatus] = useState("");
  async function action(work: () => Promise<void>) {
    setBusy(true);
    setError("");
    setStatus("");
    try {
      await work();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <Page title={name ?? "New Plugin"}>
      <Back href="plugins" label="Plugins" />
      {(error || loadError) && <FieldError>{error || loadError}</FieldError>}
      {name ? (
        data ? (
          <div className="labui:flex labui:max-w-2xl labui:flex-col labui:gap-5">
            <p>
              Download the project and open it in your editor. The project
              includes a Go plugin, React frontend, Makefile, and Dockerfile.
            </p>
            <div>
              <p className="labui:text-sm labui:text-muted-foreground">
                Project location on this server
              </p>
              <code className="labui:break-all">{data.path}</code>
            </div>
            <div className="labui:flex labui:flex-wrap labui:gap-2">
              <Button asChild>
                <a href={url(endpoint + "/download")} download>
                  <DownloadIcon />
                  Download project
                </a>
              </Button>
              <Button
                variant="secondary"
                disabled={busy}
                onClick={() =>
                  void action(async () => {
                    await navigator.clipboard.writeText(
                      pluginPrompt(
                        data.name,
                        data.path,
                        new URL(url(endpoint + "/download"), document.baseURI)
                          .href,
                      ),
                    );
                    setStatus("Prompt copied");
                  })
                }
              >
                <ClipboardIcon />
                Copy prompt
              </Button>
              <Button
                variant="secondary"
                onClick={() => open(`terminal/plugins/${data.name}`)}
              >
                <TerminalIcon />
                Open terminal
              </Button>
            </div>
            <p
              role="status"
              className="labui:text-sm labui:text-muted-foreground"
            >
              {status}
            </p>
          </div>
        ) : (
          !loadError && <p role="status">Loading project…</p>
        )
      ) : (
        <form
          className="labui:flex labui:max-w-2xl labui:flex-col labui:gap-5"
          onSubmit={(event) => {
            event.preventDefault();
            void action(async () => {
              const project = await request<Project>("POST", "/plugins", {
                name: projectName,
                module,
                files: (await import("./plugin-template")).pluginFiles(
                  projectName,
                  module,
                ),
              });
              go(`plugins/${project.name}`);
            });
          }}
        >
          <p className="labui:text-muted-foreground">
            Generate a plugin project to build with your own editor.
          </p>
          <Field>
            <FieldLabel htmlFor={id + "-name"}>Project name</FieldLabel>
            <Input
              id={id + "-name"}
              value={projectName}
              pattern="[a-z][a-z0-9-]{0,47}"
              required
              onChange={(event) => {
                const next = event.target.value;
                if (module === `example.com/${projectName}`)
                  setModule(`example.com/${next}`);
                setProjectName(next);
              }}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor={id + "-module"}>Go module</FieldLabel>
            <Input
              id={id + "-module"}
              value={module}
              required
              onChange={(event) => setModule(event.target.value)}
            />
          </Field>
          <p className="labui:text-sm labui:text-muted-foreground">
            Saved in <code>plugins/{projectName || "<project-name>"}/</code>{" "}
            inside the data directory.
          </p>
          <Button type="submit" className="labui:self-start" disabled={busy}>
            <CodeIcon />
            Create plugin
          </Button>
        </form>
      )}
    </Page>
  );
}
