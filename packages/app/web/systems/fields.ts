export function bindFields<T extends object>(
  values: T,
  change: (patch: Partial<T>) => void,
) {
  return (key: keyof T) => ({
    value: String(values[key] ?? ""),
    onChange: (value: string) =>
      change({
        [key]: typeof values[key] === "number" ? Number(value) : value,
      } as Partial<T>),
  });
}
