import { join } from "node:path";
import { testPackage } from "../../testing";

testPackage(join(import.meta.dir, ".."), {
  dependencies: ["@wterm/dom", "lucide-react", "yaml"],
  peers: {
    "@labpane/os": "^1.2.0",
    "@labpane/ui": "^1.2.0",
    "@radix-ui/react-popover": "^1.1.15",
    react: "^18.2.0 || ^19.0.0",
    "react-dom": "^18.2.0 || ^19.0.0",
  },
  entries: ["index.ts", "pages.ts"],
  budget: 125_000,
});
