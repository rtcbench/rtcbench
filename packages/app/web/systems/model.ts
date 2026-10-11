export type Network = { id: string; name: string; cidr: string };
export type Address = { networkId: string; address: string };
export type SSHKey = { id: string; name: string; path: string };
export const emptyAddress: Address = { networkId: "", address: "" };
export type Instance = {
  id: string;
  name: string;
  address: string;
  port: number;
  user: string;
  fingerprint: string;
  auth: "password" | "file";
  sudo: boolean;
  keyId?: string;
  networks?: Address[];
};
export type Deployment = {
  ports: Record<string, number>;
  overrides?: Record<string, string>;
  name: string;
  id: string;
  hostId?: string;
  networkId?: string;
  plugin: string;
  address: string;
  status: string;
  operation?: string;
  progress: string;
  error?: string;
  logs: string;
};
export type Inventory = {
  hosts: Instance[];
  networks: Network[];
  keys: SSHKey[];
  deployments: Deployment[];
  plugins: Plugin[];
};
export type Plugin = {
  webUI?: object;
  id: string;
  name: string;
  ports: {
    id: string;
    name: string;
    protocol: string;
    default: number;
    minFrom?: string;
  }[];
  credentials?: boolean;
  portNote?: string;
};
export type Credentials = Omit<Instance, "id"> & {
  id?: string;
  password: string;
};
export const initialSetup = {
  instances: {
    id: "",
    name: "",
    address: "",
    port: 22,
    user: "root",
    fingerprint: "",
    auth: "file",
    sudo: false,
    password: "",
    keyId: "",
    networks: [],
  } as Credentials,
  keys: { id: "", name: "", path: "", passphrase: "" },
  networks: { name: "", cidr: "" },
  deployments: {
    name: "",
    plugin: "",
    setup: "deploy",
    hostId: "",
    ...emptyAddress,
    apiKey: "",
    secret: "",
    ports: {} as Record<string, number>,
  },
};
export type DeploymentDraft = typeof initialSetup.deployments;
export const occupied = (data: Inventory, id: string) =>
  data.deployments.some((d) => d.hostId === id && d.status !== "stopped");
