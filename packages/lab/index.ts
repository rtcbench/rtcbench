export { Workspace, type WorkspaceTab, type WorkspaceProps } from "./workspace";
export { WorkspaceFrame } from "./workspace-frame";
export {
  densityPresets,
  densitySize,
  type Density as WorkspaceDensity,
} from "./context";
export { useUnsavedChanges } from "./unsaved-changes";
export type { TabOption } from "./toolbar";
export {
  restoreLayout,
  reconcileLayout,
  dockTab,
  panes,
  type Layout,
  type Pane,
  type Split,
} from "./layout";
