export const systemSections = {
  deployments: "Deployments",
  instances: "Instances",
  networks: "Networks",
  keys: "SSH keys",
} as const;
export type SystemSection = keyof typeof systemSections;
type Step = { section: SystemSection; id: string };

export const systemPath = (section: SystemSection, id?: string) =>
  `systems/${section}${id ? "/" + encodeURIComponent(id) : ""}`;

export function systemRoute(path = "deployments") {
  const parts = path.split("/");
  const section = parts[0] as SystemSection;
  if (!Object.hasOwn(systemSections, section)) return undefined;
  if (parts.length === 1)
    return { section, steps: [] as Step[], parent: systemPath(section) };
  if (parts.length % 2) return undefined;
  const steps: Step[] = [];
  for (let i = 0; i < parts.length; i += 2) {
    const next = parts[i] as SystemSection,
      id = parts[i + 1]!;
    if (!Object.hasOwn(systemSections, next) || !id) return undefined;
    const parent = steps.at(-1);
    if (
      parent &&
      !(
        id === "new" &&
        ((parent.section === "deployments" &&
          parent.id === "new" &&
          ["instances", "networks"].includes(next)) ||
          (parent.section === "instances" &&
            ["keys", "networks"].includes(next)))
      )
    )
      return undefined;
    steps.push({ section: next, id });
  }
  return {
    section,
    steps,
    parent:
      steps.length === 1
        ? systemPath(section)
        : "systems/" +
          steps
            .slice(0, -1)
            .map((s) => `${s.section}/${encodeURIComponent(s.id)}`)
            .join("/"),
  };
}
