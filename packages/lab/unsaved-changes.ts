import * as React from "react";

export const UnsavedChanges = React.createContext<
  ((source: symbol, dirty: boolean) => void) | undefined
>(undefined);
export function useUnsavedChanges(dirty: boolean) {
  const report = React.useContext(UnsavedChanges);
  const source = React.useRef(Symbol());
  React.useLayoutEffect(() => {
    report?.(source.current, dirty);
    return () => report?.(source.current, false);
  }, [dirty, report]);
}
