import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import postcss from "postcss";

export function unlayer(css) {
  const root = postcss.parse(css);
  for (let changed = true; changed;) {
    changed = false;
    root.walkAtRules("layer", (rule) => {
      const names = rule.params.split(",").map((name) => name.trim());
      if (!names.some((name) => name !== "properties")) return;
      if (!rule.nodes && names.includes("properties"))
        rule.params = "properties";
      else rule.replaceWith(rule.nodes ?? []);
      changed = true;
    });
  }
  return root.toString();
}

if (process.argv[1] === fileURLToPath(import.meta.url))
  for (const file of process.argv.slice(2))
    writeFileSync(file, unlayer(readFileSync(file, "utf8")));
