"use client";

import { page } from "./web/integration";
import { ConfigsPage as Configs, ConfigPage as Config } from "./web/configs";
import { EnvPage, SystemOverrides, VariableSets } from "./web/envs";
import { RunsPage as Runs } from "./web/runs";
import { RunPage as Run } from "./web/run";
import {
  BenchmarkPage as Benchmark,
  BenchmarksPage as Benchmarks,
} from "./web/benchmarks";
import { SystemsPage as Systems } from "./web/systems/page";
import { TerminalPage as Terminal } from "./web/terminal-page";
import {
  PluginsPage as Plugins,
  NewPluginPage as NewPlugin,
} from "./web/plugins";

export type { PageOptions } from "./web/integration";
export const RunsPage = /* @__PURE__ */ page(Runs);
export const RunPage = /* @__PURE__ */ page(Run);
export const BenchmarkPage = /* @__PURE__ */ page(Benchmark);
export const BenchmarksPage = /* @__PURE__ */ page(Benchmarks);
export const ConfigsPage = /* @__PURE__ */ page(Configs);
export const ConfigPage = /* @__PURE__ */ page(Config);
export const VariablesPage = /* @__PURE__ */ page(VariableSets);
export const VariablePage = /* @__PURE__ */ page(EnvPage);
export const SystemOverridesPage = /* @__PURE__ */ page(SystemOverrides);
export const SystemsPage = /* @__PURE__ */ page(Systems);
export const PluginsPage = /* @__PURE__ */ page(Plugins);
export const NewPluginPage = /* @__PURE__ */ page(NewPlugin);
export const TerminalPage = /* @__PURE__ */ page(Terminal);
