import { fileURLToPath } from "node:url";

const aliases = {
  "@rtcbench/lab": "../lab/index.ts",
  "@rtcbench/admin": "../admin/index.ts",
  react: "preact/compat",
  "react-dom": "preact/compat",
  "react/jsx-runtime": "preact/jsx-runtime",
  "react/jsx-dev-runtime": "preact/jsx-dev-runtime",
};

export default {
  name: "rtcbench-dev",
  setup(build) {
    build.onResolve(
      {
        filter:
          /^(react(?:-dom|\/jsx(?:-dev)?-runtime)?|@rtcbench\/(lab|admin))$/,
      },
      ({ path }) => ({
        path: fileURLToPath(import.meta.resolve(aliases[path])),
      }),
    );
  },
};
