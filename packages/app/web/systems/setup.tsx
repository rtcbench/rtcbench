import { Back, useNavigation, useUnsavedChanges } from "../integration";
import {
  systemPath,
  systemRoute,
  systemSections,
  type SystemSection,
} from "./routes";
import { InstanceFields } from "./instance-fields";
import { DeploymentFields } from "./deployment-fields";
import { useEffect, useState, useRef } from "react";
import { PlayIcon, SaveIcon, TerminalIcon } from "lucide-react";
import { useApi } from "../api";

import {
  Button,
  FieldError,
  PageHeader,
  Spinner,
  ConfirmDialog,
  TextField,
  AddButton,
} from "@labpane/ui";

import {
  emptyAddress,
  initialSetup,
  occupied,
  type Inventory,
  type Address,
} from "./model";

export function SystemSetup({
  path,
  route,
  data,
  reload,
  saved,
  cancel,
}: {
  path: string;
  route: NonNullable<ReturnType<typeof systemRoute>>;
  data: Inventory;
  reload: () => Promise<void>;
  saved: () => void;
  cancel?: () => void;
}) {
  const { request } = useApi();
  const { go, open } = useNavigation();
  const { steps, parent } = route;
  const root = steps[0]!,
    current = steps.at(-1)!;
  const step = current.section;
  const titleFor = (entry: typeof current) =>
    entry.section === "deployments"
      ? "New deployment"
      : `${entry.id === "new" ? "Add" : "Edit"} ${entry.section === "instances" ? "instance" : entry.section === "keys" ? "SSH key" : "network"}`;
  const parentLabel =
    steps.length > 1 ? titleFor(steps.at(-2)!) : systemSections[root.section];
  const [drafts, setDrafts] = useState(() => {
    const first = data.hosts.find((h) => !occupied(data, h.id));
    return {
      ...initialSetup,
      instances: {
        ...initialSetup.instances,
        keyId: data.keys[0]?.id ?? "",
        ...data.hosts.find(
          (h) => root.section === "instances" && h.id === root.id,
        ),
      },
      keys: {
        ...initialSetup.keys,
        ...data.keys.find((k) => root.section === "keys" && k.id === root.id),
      },
      deployments: {
        ...initialSetup.deployments,
        plugin: data.plugins[0]?.id ?? "",
        hostId: first?.id ?? "",
        ...(first?.networks?.[0] ?? emptyAddress),
      },
    };
  });
  const { instances: instance, deployments: deployment } = drafts;
  function update<K extends SystemSection>(
    section: K,
    patch: Partial<(typeof drafts)[K]>,
  ) {
    setDrafts((all) => ({ ...all, [section]: { ...all[section], ...patch } }));
  }
  const [busy, setBusy] = useState(false),
    [error, setError] = useState<string>();
  const snapshot = JSON.stringify(drafts);
  const original = useRef(snapshot);
  useUnsavedChanges(snapshot !== original.current);
  useEffect(() => setError(undefined), [path]);
  const host = data.hosts.find((h) => h.id === deployment.hostId),
    external = deployment.setup === "existing";
  const push = (next: SystemSection) => go(`systems/${path}/${next}/new`);
  const title = titleFor(current);
  const value = drafts[step];
  const endpoint = {
    instances: "/systems/instances",
    keys: "/systems/keys",
    networks: "/systems/networks",
    deployments: "/deployments",
  }[step];
  const id = "id" in value ? value.id : undefined;
  const used =
    step === "keys"
      ? data.hosts.some((h) => h.keyId === id)
      : data.deployments.some((d) => d.hostId === id);
  const addNetwork = (
    <AddButton onClick={() => push("networks")}>Add network</AddButton>
  );
  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(undefined);
    try {
      const { setup, ...params } = deployment;
      const payload =
        step === "deployments"
          ? { ...params, hostId: external ? "" : deployment.hostId }
          : value;
      const result = await request<{ id: string; networks?: Address[] }>(
        id ? "PUT" : "POST",
        endpoint + (id ? `/${id}` : ""),
        payload,
      );
      const destination =
        steps.length === 1 ? systemPath(step, result.id) : parent;
      if (step === "networks") {
        if (steps.at(-2)?.section === "instances")
          update("instances", {
            networks: [
              ...(instance.networks ?? []),
              { networkId: result.id, address: "" },
            ],
          });
        else update("deployments", { networkId: result.id, address: "" });
      } else if (step === "keys") {
        update("instances", { keyId: result.id, auth: "file" });
      } else if (step === "instances") {
        update("deployments", {
          hostId: result.id,
          ...(result.networks?.[0] ?? emptyAddress),
        });
      }
      update(
        step,
        step === "deployments" ? { secret: "" } : initialSetup[step],
      );
      await reload();
      if (steps.length === 1) saved();
      go(destination);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <form onSubmit={submit} className="labui:flex labui:flex-col labui:gap-6">
      <Back
        nested={steps.length > 1}
        href={parent}
        label={parentLabel}
        onClick={(e) => {
          if (busy) e.preventDefault();
        }}
      />
      <PageHeader
        title={title}
        actions={
          <>
            {cancel && (
              <Button
                type="button"
                variant="secondary"
                disabled={busy}
                onClick={cancel}
              >
                Cancel
              </Button>
            )}
            {step === "instances" && current.id !== "new" && (
              <Button
                type="button"
                variant="secondary"
                onClick={() => open(`terminal/instances/${current.id}`)}
              >
                <TerminalIcon />
                Open terminal
              </Button>
            )}
            <Button
              type="submit"
              disabled={
                busy ||
                (step === "deployments" &&
                  (!deployment.plugin ||
                    !deployment.address ||
                    (!external && (!host || occupied(data, host.id))))) ||
                (step === "instances" &&
                  instance.auth === "file" &&
                  !instance.keyId)
              }
            >
              {busy ? (
                <Spinner />
              ) : step === "deployments" ? (
                <PlayIcon />
              ) : (
                <SaveIcon />
              )}
              {step === "deployments"
                ? external
                  ? "Connect"
                  : "Deploy"
                : "Save"}
            </Button>
          </>
        }
      />
      <fieldset
        disabled={busy}
        className="labui:flex labui:max-w-2xl labui:min-w-0 labui:flex-col labui:gap-4"
        key={step}
      >
        {step === "networks" || step === "keys" ? (
          Object.entries(
            step === "networks"
              ? { name: "Name", cidr: "CIDR" }
              : {
                  name: "Name",
                  path: "Key file on controller",
                  passphrase: "Passphrase (optional)",
                },
          ).map(([field, label]) => {
            const values: Record<string, string> = drafts[step];
            return (
              <TextField
                key={field}
                label={label}
                value={values[field]!}
                onChange={(value) => update(step, { [field]: value })}
                type={field === "passphrase" ? "password" : "text"}
                autoComplete={field === "passphrase" ? "new-password" : "off"}
                required={field !== "passphrase"}
              />
            );
          })
        ) : step === "instances" ? (
          <InstanceFields
            value={instance}
            change={(patch) => update("instances", patch)}
            data={data}
            addNetwork={addNetwork}
            addKey={() => push("keys")}
          />
        ) : (
          <DeploymentFields
            value={deployment}
            change={(patch) => update("deployments", patch)}
            data={data}
            addNetwork={addNetwork}
            addInstance={() => {
              update("instances", {
                ...initialSetup.instances,
                keyId: data.keys[0]?.id ?? "",
              });
              push("instances");
            }}
          />
        )}
      </fieldset>
      {id && (
        <ConfirmDialog
          title="Remove"
          name={value.name}
          confirmLabel="Remove"
          trigger={
            <Button
              type="button"
              variant="destructive"
              className="labui:self-start"
              disabled={busy || used}
            >
              Remove
            </Button>
          }
          onConfirm={async () => {
            await request("DELETE", `${endpoint}/${id}`);
            await reload();
            go(parent);
          }}
        />
      )}
      {error && <FieldError>{error}</FieldError>}
    </form>
  );
}
