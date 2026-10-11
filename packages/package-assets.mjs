import { copyFileSync } from "node:fs";

copyFileSync("../../LICENSE", "dist/LICENSE");
copyFileSync("../styles.d.ts", "dist/styles.d.ts");
for (const name of process.argv.slice(2)) {
  copyFileSync(
    new URL("../LICENSE", import.meta.resolve(name)),
    `dist/${name.replace(/^@/, "").replaceAll("/", "-")}-LICENSE`,
  );
}
