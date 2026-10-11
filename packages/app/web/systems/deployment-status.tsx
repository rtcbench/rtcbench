import { Badge, Spinner } from "@rtcbench/admin";
import type { Deployment } from "./model";

const labels: Record<string, string> = {
  deploy: "Deploying",
  stop: "Stopping",
  refresh: "Checking health",
  logs: "Loading logs",
  ready: "Ready",
  configured: "External",
  failed: "Failed",
  stopped: "Stopped",
  unknown: "Needs health check",
};
export function DeploymentStatus({
  deployment: d,
}: {
  deployment: Deployment;
}) {
  return (
    <Badge
      variant={
        d.error
          ? "destructive"
          : ["ready", "configured"].includes(d.status)
            ? "success"
            : "warning"
      }
    >
      {d.operation && <Spinner className="size-3" />}
      {labels[d.operation || d.status] ?? d.status}
    </Badge>
  );
}
