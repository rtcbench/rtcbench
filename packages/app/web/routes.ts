export type Route = { page: string; id?: string; tabKey?: string };
export const routePath = ({ page, id }: Route) =>
  page + (id ? "/" + id.split("/").map(encodeURIComponent).join("/") : "");

const pattern =
  /^\/(welcome|workspace|runs|run|systems|configs|config|environment|plugins|terminal)(?:\/(.+))?$/;
export function parse(path: string) {
  const match = ("/" + path.replace(/^\//, "")).match(pattern);
  if (!match || path.endsWith(".yml")) return null;
  try {
    const page = match[1]!;
    const id = match[2] ? decodeURIComponent(match[2]) : undefined;
    if (
      page === "terminal" &&
      id &&
      !/^(plugins|instances|deployments)\/[a-zA-Z0-9-]+$/.test(id)
    )
      return null;
    if (page === "plugins" && id && !/^[a-z][a-z0-9-]{0,47}$/.test(id))
      return null;
    return { page, id: page === "systems" ? (id ?? "deployments") : id };
  } catch {
    return null;
  }
}
