import { useNavigation, useUnsavedChanges, Back } from "../integration";
import { systemPath } from "./routes";
import { useState } from "react";
import {
  ExternalLinkIcon,
  PlayIcon,
  SquareIcon,
  RefreshCwIcon,
  TerminalIcon,
  Trash2Icon,
} from "lucide-react";
import { useApi } from "../api";

import { SystemOverrides } from "../envs";
import {
  mono,
  Table,
  Button,
  CopyButton,
  FieldError,
  PageActions,
  PageHeader,
  ConfirmDialog,
} from "@labpane/ui";

import { NewRun } from "../runs";
import type { Deployment, Instance, Plugin } from "./model";
import { DeploymentStatus } from "./deployment-status";
import { PortFields } from "./port-fields";

type DetailProps = {
  deployment: Deployment;
  plugin?: Plugin;
  host?: Instance;
  network?: string;
  reload: () => Promise<void>;
};
export function DeploymentDetails({
  deployment: d,
  plugin,
  host,
  network,
  reload,
}: DetailProps) {
  const { request, url } = useApi();
  const { go, open } = useNavigation();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string>();
  const [showLogs, setShowLogs] = useState(false);
  const [ports, setPorts] = useState<Record<string, number>>();
  useUnsavedChanges(ports !== undefined);
  const busy = pending || !!d.operation;
  const removable = !d.hostId || d.status === "stopped";
  async function action(name: string, body?: Record<string, number>) {
    setPending(true);
    setError(undefined);
    try {
      await request(
        name === "remove" ? "DELETE" : name === "ports" ? "PUT" : "POST",
        `/deployments/${d.id}${name === "remove" ? "" : "/" + name}`,
        body,
      );
      if (name === "logs") setShowLogs(true);
      await reload();
      if (name === "ports") setPorts(undefined);
      if (name === "remove") go(systemPath("deployments"));
    } finally {
      setPending(false);
    }
  }
  return (
    <div className="labui:flex labui:flex-col labui:gap-6">
      <Back href={systemPath("deployments")} label="Deployments" />
      <PageHeader
        title={d.name}
        actions={
          <>
            {d.hostId &&
              plugin?.webUI &&
              ["ready", "unknown"].includes(d.status) && (
                <Button variant="secondary" asChild>
                  <a
                    href={url(`/deployments/${d.id}/web`)}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    <ExternalLinkIcon />
                    Open web UI
                  </a>
                </Button>
              )}
            {d.hostId && (
              <Button
                variant="secondary"
                onClick={() => open(`terminal/deployments/${d.id}`)}
              >
                <TerminalIcon />
                Open terminal
              </Button>
            )}
            {["ready", "configured"].includes(d.status) && (
              <NewRun deploymentId={d.id} />
            )}
          </>
        }
      />
      <div className="labui:flex labui:flex-wrap labui:items-center labui:justify-between labui:gap-3">
        <span className="labui:text-sm">
          {host?.name ?? "Existing SFU"} | {network && `${network} | `}
          {d.address}
          <CopyButton value={d.address} label="Copy traffic IP" />
        </span>
        <DeploymentStatus deployment={d} />
      </div>
      {d.operation && (
        <p role="status" className="labui:text-sm">
          {d.progress}
        </p>
      )}
      {(error || d.error) && <FieldError>{error || d.error}</FieldError>}
      <form
        className="labui:flex labui:max-w-2xl labui:flex-col labui:gap-4"
        onSubmit={async (event) => {
          event.preventDefault();
          await action("ports", ports).catch((err: Error) =>
            setError(err.message),
          );
        }}
      >
        <PortFields
          plugin={plugin}
          value={ports ?? d.ports}
          change={setPorts}
          disabled={busy || !removable}
        >
          <p className="labui:mt-4 labui:text-sm labui:text-muted-foreground">
            {d.hostId
              ? "Stop the deployment to change ports, then deploy again to apply them."
              : "Match the existing SFU. Saving here does not reconfigure that server."}
          </p>
          {ports && (
            <PageActions>
              <Button type="submit" size="sm" disabled={busy || !removable}>
                Save ports
              </Button>
              <Button
                type="button"
                size="sm"
                variant="secondary"
                disabled={busy}
                onClick={() => setPorts(undefined)}
              >
                Cancel
              </Button>
            </PageActions>
          )}
        </PortFields>
      </form>
      <section className="labui:flex labui:flex-col labui:gap-3">
        <h2 className="labui:text-sm labui:font-semibold">System overrides</h2>
        <Table
          label="System overrides"
          rows={Object.entries(d.overrides ?? {})}
          rowKey={([key]) => key}
          columns={[
            mono<[string, string]>("setting", "Setting", ([key]) => key),
            mono<[string, string]>("value", "Value", ([, value]) => value),
          ]}
        />
      </section>
      <PageActions>
        {d.hostId &&
          (
            [
              d.status === "stopped"
                ? (["retry", "Deploy again", PlayIcon] as const)
                : (["refresh", "Check health", RefreshCwIcon] as const),
              ["logs", "Logs", TerminalIcon],
            ] as const
          ).map(([name, label, Icon]) => (
            <Button
              key={name}
              size="sm"
              variant="secondary"
              disabled={busy || ports !== undefined}
              onClick={() =>
                action(name).catch((err: Error) => setError(err.message))
              }
            >
              <Icon />
              {label}
            </Button>
          ))}
        <ConfirmDialog
          title={removable ? "Remove deployment" : "Stop deployment"}
          name={`${plugin?.name ?? d.plugin} | ${d.address}`}
          confirmLabel={removable ? "Remove" : "Stop"}
          trigger={
            <Button size="sm" variant="destructive" disabled={busy}>
              {removable ? <Trash2Icon /> : <SquareIcon />}
              {removable ? "Remove" : "Stop"}
            </Button>
          }
          onConfirm={() => action(removable ? "remove" : "stop")}
        />
      </PageActions>
      {(showLogs || d.status === "failed") && (
        <pre
          aria-label="Operation output"
          className="rtc-code-block labui:max-h-64 labui:overflow-auto labui:whitespace-pre-wrap labui:break-all"
        >
          {d.logs || (d.operation ? "Loading..." : "No output")}
        </pre>
      )}
      <SystemOverrides name={`plugins/${d.id}`} />
    </div>
  );
}
