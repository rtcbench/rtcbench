import { useId, type ReactNode } from "react";
import { Trash2Icon } from "lucide-react";
import {
  TextField,
  SelectField,
  AddButton,
  Button,
  Checkbox,
  Field,
  FieldLabel,
} from "@rtcbench/admin";

import type { Address, Credentials, Inventory } from "./model";
import { bindFields } from "./fields";

export function InstanceFields({
  value,
  change,
  data,
  addNetwork,
  addKey,
}: {
  value: Credentials;
  change: (patch: Partial<Credentials>) => void;
  data: Inventory;
  addNetwork: ReactNode;
  addKey: () => void;
}) {
  const sudoId = useId();
  const field = bindFields(value, change);
  return (
    <>
      <TextField label="Name" {...field("name")} required />
      <TextField label="SSH address" {...field("address")} required />
      <TextField
        label="SSH port"
        type="number"
        min={1}
        max={65535}
        {...field("port")}
        required
      />
      <TextField label="Username" {...field("user")} required />
      <SelectField
        label="Authentication"
        value={value.auth}
        onChange={(auth) =>
          change({
            auth: auth as Credentials["auth"],
            keyId: auth === "file" ? data.keys[0]?.id : "",
          })
        }
        options={[
          { id: "file", name: "SSH key" },
          { id: "password", name: "Password" },
        ]}
      />
      {value.auth === "file" ? (
        <SelectField label="SSH key" {...field("keyId")} options={data.keys}>
          <AddButton onClick={addKey}>Add key</AddButton>
        </SelectField>
      ) : (
        <TextField
          label="SSH password"
          type="password"
          autoComplete="new-password"
          {...field("password")}
          required={!value.id}
        />
      )}
      <TextField
        label="SSH host-key fingerprint"
        {...field("fingerprint")}
        required
      />
      <details className="text-xs text-muted-foreground">
        <summary>Fingerprint command</summary>
        <code>ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub -E sha256</code>
      </details>
      <Field orientation="horizontal">
        <Checkbox
          id={sudoId}
          checked={value.sudo}
          onCheckedChange={(v) => change({ sudo: v === true })}
        />
        <FieldLabel htmlFor={sudoId}>Passwordless sudo</FieldLabel>
      </Field>
      <div className="flex items-center justify-between">
        <span className="text-sm font-medium">Network addresses</span>
        {addNetwork}
      </div>
      {(value.networks ?? []).map((address, index) => {
        const update = (patch?: Partial<Address>) =>
          change({
            networks: value.networks!.flatMap((a, i) =>
              i !== index ? [a] : patch ? [{ ...a, ...patch }] : [],
            ),
          });
        const field = bindFields(address, update);
        return (
          <div className="flex items-end gap-2" key={index}>
            <SelectField
              label="Network"
              {...field("networkId")}
              options={data.networks}
            />
            <TextField label="Traffic IP" {...field("address")} required />
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label="Remove address"
              onClick={() => update()}
            >
              <Trash2Icon />
            </Button>
          </div>
        );
      })}
      {!!data.networks.length && (
        <AddButton
          onClick={() =>
            change({
              networks: [
                ...(value.networks ?? []),
                { networkId: data.networks[0]!.id, address: "" },
              ],
            })
          }
        >
          Add address
        </AddButton>
      )}
    </>
  );
}
