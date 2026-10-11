import type { ReactNode } from "react";
import { AddButton, SelectField, TextField, FieldError } from "@rtcbench/admin";

import {
  emptyAddress,
  occupied,
  type Inventory,
  type DeploymentDraft,
} from "./model";
import { PortFields } from "./port-fields";
import { bindFields } from "./fields";

export function DeploymentFields({
  value,
  change,
  data,
  addNetwork,
  addInstance,
}: {
  value: DeploymentDraft;
  change: (patch: Partial<DeploymentDraft>) => void;
  data: Inventory;
  addNetwork: ReactNode;
  addInstance: () => void;
}) {
  const field = bindFields(value, change);
  const external = value.setup === "existing";
  const plugin = data.plugins.find((p) => p.id === value.plugin);
  const host = data.hosts.find((h) => h.id === value.hostId);
  const addresses = !external ? host?.networks : undefined;
  return (
    <>
      <TextField label="Name" {...field("name")} required />
      <SelectField
        label="Plugin"
        value={value.plugin}
        onChange={(plugin) => change({ plugin, ports: {} })}
        options={data.plugins}
      />
      <SelectField
        label="Setup"
        value={value.setup}
        onChange={(setup) =>
          change({
            setup,
            ...(setup === "existing"
              ? emptyAddress
              : (host?.networks?.[0] ?? emptyAddress)),
          })
        }
        options={[
          { id: "deploy", name: "Deploy on instance" },
          { id: "existing", name: "Connect to existing" },
        ]}
      />
      {!external && (
        <SelectField
          label="Instance"
          value={value.hostId}
          onChange={(hostId) =>
            change({
              hostId,
              ...(data.hosts.find((h) => h.id === hostId)?.networks?.[0] ??
                emptyAddress),
            })
          }
          options={data.hosts.map((h) => ({
            id: h.id,
            name: h.name + (occupied(data, h.id) ? " | In use" : ""),
          }))}
        >
          <AddButton onClick={addInstance}>Add instance</AddButton>
        </SelectField>
      )}
      <SelectField
        label="Network"
        value={value.networkId || "."}
        onChange={(networkId) =>
          change(
            addresses?.find((a) => a.networkId === networkId) ?? {
              networkId: networkId === "." ? "" : networkId,
            },
          )
        }
        options={
          addresses?.length
            ? addresses.map((a) => ({
                id: a.networkId,
                name: `${data.networks.find((n) => n.id === a.networkId)?.name} | ${a.address}`,
              }))
            : [{ id: ".", name: "Unspecified" }, ...data.networks]
        }
      >
        {!addresses?.length && addNetwork}
      </SelectField>
      {!addresses?.length && (
        <TextField label="Traffic IP" {...field("address")} required />
      )}
      {external && plugin?.credentials && (
        <>
          <TextField label="API key" {...field("apiKey")} required />
          <TextField
            label="API secret"
            type="password"
            autoComplete="new-password"
            {...field("secret")}
            required
          />
        </>
      )}
      <PortFields
        plugin={plugin}
        value={value.ports}
        change={(ports) => change({ ports })}
      >
        {external && (
          <p className="mt-4 text-sm text-muted-foreground">
            Match the existing SFU. Saving here does not reconfigure that
            server.
          </p>
        )}
      </PortFields>
      {!external && host && occupied(data, host.id) && (
        <FieldError>Instance in use</FieldError>
      )}
    </>
  );
}
