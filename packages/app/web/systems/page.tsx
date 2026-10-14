import { useNavigation, usePageTitle, Back } from "../integration";
import { systemPath, systemRoute, systemSections } from "./routes";

import { useState } from "react";
import {
  KeyRoundIcon,
  PlusIcon,
  Trash2Icon,
  PencilIcon,
  TerminalIcon,
} from "lucide-react";
import { useApi, usePoll } from "../api";
import {
  column,
  Table,
  type TableColumn,
  Button,
  FieldError,
  Page,
  PageHeader,
  Spinner,
  ConfirmDialog,
} from "@labpane/ui";

import { SystemSetup } from "./setup";
import { DeploymentDetails } from "./deployment-details";
import { DeploymentStatus } from "./deployment-status";
import type { Inventory, Network, Deployment } from "./model";

export function SystemsPage({ path = "deployments" }: { path?: string }) {
  const { open } = useNavigation();
  const route = systemRoute(path);
  if (!route)
    return (
      <Page title="Systems">
        <FieldError>Page not found</FieldError>
        <Back href={systemPath("deployments")} label="Deployments" />
      </Page>
    );
  const { section, steps } = route;
  const detail = steps.length > 0;
  return (
    <Page
      title={detail ? undefined : systemSections[section]}
      actions={
        <>
          {section === "instances" && (
            <Button
              variant="secondary"
              onClick={() => open(systemPath("keys"))}
            >
              <KeyRoundIcon />
              SSH keys
            </Button>
          )}
          <Button
            variant={section === "deployments" ? "default" : "secondary"}
            onClick={() => open(systemPath(section, "new"))}
          >
            <PlusIcon />
            {
              {
                deployments: "New deployment",
                instances: "Add instance",
                networks: "Add network",
                keys: "Add key",
              }[section]
            }
          </Button>
        </>
      }
    >
      <SystemContents
        key={`${section}/${steps[0]?.id ?? ""}`}
        path={path}
        route={route}
      />
    </Page>
  );
}

function SystemContents({
  path,
  route,
}: {
  path: string;
  route: NonNullable<ReturnType<typeof systemRoute>>;
}) {
  const { request } = useApi();
  const { open } = useNavigation();
  const { data, error, reload } = usePoll<Inventory>("/systems", 2500);
  const { section, steps } = route;
  const {
    hosts = [],
    keys = [],
    networks = [],
    deployments = [],
    plugins = [],
  } = data ?? {};
  const root = steps[0];
  const [editing, setEditing] = useState(false);
  const resource = { instances: hosts, keys, networks, deployments }[
    section
  ].find((item) => item.id === root?.id);
  const table = <T extends { id: string; name: string }>(
    rows: T[],
    fields: Record<string, (row: T) => string | number | undefined>,
    actions: TableColumn<T>[] = [],
  ) => (
    <Table
      label={systemSections[section]}
      empty="Empty"
      rows={rows}
      loading={!data}
      rowKey={(row) => row.id}
      rowLabel={(row) => row.name}
      onRowActivate={(row) => open(systemPath(section, row.id))}
      columns={[
        column<T>("name", "Name", (row) => row.name),
        ...Object.entries(fields).map(([label, value]) =>
          column<T>(label, label, value),
        ),
        ...actions,
      ]}
    />
  );
  usePageTitle(resource?.name);
  if (root && !data)
    return error ? <FieldError>{error}</FieldError> : <Spinner />;
  if (data && root && root.id !== "new" && !resource)
    return (
      <>
        <Back href={systemPath(section)} label={systemSections[section]} />
        <FieldError>Resource not found</FieldError>
      </>
    );
  if (data && root && (root.id === "new" || steps.length > 1 || editing))
    return (
      <SystemSetup
        key={`${root.section}/${root.id}`}
        path={path}
        route={route}
        data={data}
        reload={reload}
        saved={() => setEditing(false)}
        cancel={
          root.id === "new" || steps.length > 1
            ? undefined
            : () => setEditing(false)
        }
      />
    );
  const hostName = (id?: string) => hosts.find((h) => h.id === id)?.name;
  const networkName = (id?: string) => networks.find((n) => n.id === id)?.name;
  if (resource && "plugin" in resource)
    return (
      <DeploymentDetails
        key={resource.id}
        deployment={resource}
        plugin={plugins.find((p) => p.id === resource.plugin)}
        host={hosts.find((h) => h.id === resource.hostId)}
        network={networkName(resource.networkId)}
        reload={reload}
      />
    );
  if (resource && data) {
    const host = "user" in resource ? resource : undefined;
    const fields = host
      ? {
          "SSH address": `${host.user}@${host.address}:${host.port}`,
          Authentication:
            host.auth === "password"
              ? "Password"
              : keys.find((k) => k.id === host.keyId)?.name,
          "Host fingerprint": host.fingerprint,
          Networks: host.networks
            ?.map((n) => `${networkName(n.networkId)}: ${n.address}`)
            .join(", "),
        }
      : "path" in resource
        ? { "Key file on controller": resource.path }
        : { CIDR: "cidr" in resource ? resource.cidr : undefined };
    return (
      <>
        <Back href={systemPath(section)} label={systemSections[section]} />
        <PageHeader
          title={resource.name}
          actions={
            <>
              {host && (
                <Button
                  variant="secondary"
                  onClick={() => open(`terminal/instances/${host.id}`)}
                >
                  <TerminalIcon />
                  Open terminal
                </Button>
              )}
              {section !== "networks" && (
                <Button onClick={() => setEditing(true)}>
                  <PencilIcon />
                  Edit
                </Button>
              )}
            </>
          }
        />
        <dl className="labui:grid labui:grid-cols-[auto_minmax(0,1fr)] labui:gap-x-6 labui:gap-y-3 labui:text-sm">
          {Object.entries(fields)
            .filter(([, value]) => value)
            .map(([label, value]) => (
              <div key={label} className="labui:contents">
                <dt className="labui:text-muted-foreground">{label}</dt>
                <dd className="labui:break-all">{value}</dd>
              </div>
            ))}
        </dl>
      </>
    );
  }
  return (
    <div className="labui:flex labui:flex-col labui:gap-6">
      {error && <FieldError>{error}</FieldError>}
      {section === "keys" && (
        <>
          <Back href={systemPath("instances")} label="Instances" />
          {table(keys, {
            "Key file on controller": (key) => key.path,
            Instances: (key) => hosts.filter((h) => h.keyId === key.id).length,
          })}
        </>
      )}
      {section === "deployments" &&
        table(
          deployments,
          {
            Plugin: (d) =>
              plugins.find((p) => p.id === d.plugin)?.name ?? d.plugin,
            Instance: (d) => hostName(d.hostId) ?? "External",
            Network: (d) => networkName(d.networkId),
            "Traffic IP": (d) => d.address,
          },
          [
            column<Deployment>(
              "status",
              "Status",
              (d) => d.status,
              (d) => <DeploymentStatus deployment={d} />,
            ),
          ],
        )}
      {section === "instances" &&
        table(hosts, {
          "SSH address": (h) => `${h.user}@${h.address}:${h.port}`,
          Networks: (h) =>
            h.networks?.map((i) => networkName(i.networkId)).join(", "),
          "SSH key": (h) =>
            keys.find((k) => k.id === h.keyId)?.name ??
            (h.auth === "password" ? "Password" : "Saved key"),
          Usage: (h) =>
            deployments.find((d) => d.hostId === h.id && d.status !== "stopped")
              ?.name ?? "Available",
        })}
      {section === "networks" &&
        table(
          networks,
          {
            CIDR: (n) => n.cidr,
            Instances: (n) =>
              hosts.filter((h) => h.networks?.some((i) => i.networkId === n.id))
                .length,
            Deployments: (n) =>
              deployments.filter((d) => d.networkId === n.id).length,
          },
          [
            column<Network>(
              "remove",
              "Actions",
              (n) => n.name,
              (n) => (
                <ConfirmDialog
                  title="Remove network"
                  name={n.name}
                  confirmLabel="Remove"
                  trigger={
                    <Button
                      size="icon-sm"
                      variant="ghost"
                      aria-label={`Remove ${n.name}`}
                      disabled={
                        hosts.some((h) =>
                          h.networks?.some((i) => i.networkId === n.id),
                        ) || deployments.some((d) => d.networkId === n.id)
                      }
                    >
                      <Trash2Icon />
                    </Button>
                  }
                  onConfirm={async () => {
                    await request("DELETE", `/systems/networks/${n.id}`);
                    await reload();
                  }}
                />
              ),
            ),
          ],
        )}
    </div>
  );
}
