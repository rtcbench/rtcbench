import { lazy, Suspense } from "react";

const Terminal = lazy(() => import("./terminal"));

export function TerminalPage({ target }: { target?: string }) {
  return (
    <Suspense fallback={<p role="status">Loading terminal…</p>}>
      <Terminal target={target} />
    </Suspense>
  );
}
