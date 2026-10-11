import { expect, it } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { DeploymentFields } from "./deployment-fields";
import { initialSetup, type Inventory } from "./model";

it("renders custom plugin credentials, port ranges, and guidance from metadata", () => {
  const data: Inventory = {
    hosts: [],
    keys: [],
    networks: [],
    deployments: [],
    plugins: [
      {
        id: "custom",
        name: "Custom",
        credentials: true,
        portNote: "Custom port guidance",
        ports: [
          { id: "first", name: "First", protocol: "UDP", default: 9000 },
          {
            id: "last",
            name: "Last",
            protocol: "UDP",
            default: 9010,
            minFrom: "first",
          },
        ],
      },
    ],
  };
  const render = () =>
    renderToStaticMarkup(
      <DeploymentFields
        value={{
          ...initialSetup.deployments,
          plugin: "custom",
          setup: "existing",
        }}
        change={() => {}}
        data={data}
        addNetwork={null}
        addInstance={() => {}}
      />,
    );
  expect(render()).toContain("API key");
  expect(render()).toContain("API secret");
  expect(render()).toContain("Custom port guidance");
  expect(render()).toContain('min="9000"');
  data.plugins[0]!.credentials = false;
  expect(render()).not.toContain("API secret");
});
