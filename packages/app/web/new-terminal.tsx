import { useState } from "react";
import { TerminalIcon } from "lucide-react";
import {
  Button,
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  FieldError,
  SelectField,
  Spinner,
} from "@rtcbench/admin";
import { usePoll } from "./api";
import type { Inventory } from "./systems/model";

export function NewTerminal({
  close,
  open,
}: {
  close: () => void;
  open: (path: string) => void;
}) {
  const [kind, setKind] = useState("server");
  const [id, setId] = useState("");
  const inventory = usePoll<Inventory>(
    kind === "instances" || kind === "deployments" ? "/systems" : null,
  );
  const projects = usePoll<{ project?: string }[]>(
    kind === "plugins" ? "/plugins" : null,
  );
  const targets =
    kind === "plugins"
      ? projects.data?.flatMap((p) =>
          p.project ? [{ id: p.project, name: p.project }] : [],
        )
      : kind === "instances"
        ? inventory.data?.hosts
        : inventory.data?.deployments.filter((d) => d.hostId);
  const error = inventory.error || projects.error;
  return (
    <Dialog
      open
      onOpenChange={(next) => {
        if (!next) close();
      }}
    >
      <DialogContent aria-describedby={undefined}>
        <form
          className="flex flex-col gap-5"
          onSubmit={(event) => {
            event.preventDefault();
            if (
              kind !== "server" &&
              !targets?.some((target) => target.id === id)
            )
              return;
            open(
              kind === "server"
                ? "terminal"
                : `terminal/${kind}/${encodeURIComponent(id)}`,
            );
            close();
          }}
        >
          <DialogHeader>
            <DialogTitle>New Terminal</DialogTitle>
          </DialogHeader>
          <SelectField
            label="Type"
            value={kind}
            onChange={(next) => {
              setKind(next);
              setId("");
            }}
            options={[
              { id: "server", name: "Server" },
              { id: "instances", name: "Instance (SSH)" },
              { id: "deployments", name: "Deployment (container)" },
              { id: "plugins", name: "Plugin project" },
            ]}
          />
          {kind !== "server" &&
            (targets ? (
              targets.length ? (
                <SelectField
                  label="Target"
                  value={id}
                  onChange={setId}
                  options={targets}
                />
              ) : (
                <p className="text-sm text-muted-foreground">
                  No {kind === "plugins" ? "plugin projects" : kind} available
                </p>
              )
            ) : (
              !error && <Spinner />
            ))}
          {error && <FieldError>{error}</FieldError>}
          <DialogFooter>
            <Button
              type="submit"
              disabled={
                kind !== "server" &&
                !targets?.some((target) => target.id === id)
              }
            >
              <TerminalIcon />
              Open terminal
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
