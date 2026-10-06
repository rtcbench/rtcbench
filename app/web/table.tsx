import * as React from "react";
import { type ReactNode } from "react";
import { ArrowDownIcon, ArrowUpDownIcon, ArrowUpIcon } from "lucide-react";
import { show } from "./api";
import { Button, cn, Skeleton } from "./ui";

export type TableColumn<T> = {
  id: string;
  header: string;
  headerDetail?: string;
  rowHeader?: boolean;
  numeric?: boolean;
  cell: (row: T) => React.ReactNode;
  sortValue: (row: T) => string | number | null | undefined;
};

type Sort = { column: string; direction: "asc" | "desc" };

const cellClass = "px-3 py-(--row-py) align-middle";

export function Table<T>({
  label,
  columns,
  rows,
  rowKey,
  loading,
  empty = "No rows to display.",
  defaultSort = null,
  onRowActivate,
  rowLabel,
}: {
  label: string;
  columns: TableColumn<T>[];
  rows: readonly T[];
  rowKey: (row: T, index: number) => string;
  loading?: boolean;
  empty?: React.ReactNode;
  defaultSort?: Sort | null;
  onRowActivate?: (row: T) => void;
  rowLabel?: (row: T) => string;
}) {
  const [sort, setSort] = React.useState(defaultSort);
  const sortBy = columns.find((c) => c.id === sort?.column)?.sortValue;
  const sorted =
    sort && sortBy
      ? [...rows].sort((x, y) => {
          const a = sortBy(x);
          const b = sortBy(y);
          if (a == null || b == null)
            return Number(a == null) - Number(b == null);
          const order =
            typeof a === "number" && typeof b === "number"
              ? a - b
              : String(a).localeCompare(String(b), undefined, {
                  numeric: true,
                });
          return sort.direction === "asc" ? order : -order;
        })
      : rows;

  function toggle(column: TableColumn<T>) {
    setSort(
      sort?.column === column.id
        ? {
            column: column.id,
            direction: sort.direction === "asc" ? "desc" : "asc",
          }
        : { column: column.id, direction: column.numeric ? "desc" : "asc" },
    );
  }

  return (
    <div className="min-w-0 overflow-auto rounded-lg border">
      <table
        aria-label={label}
        aria-busy={loading || undefined}
        className="w-full text-xs tabular-nums"
      >
        <thead>
          <tr>
            {columns.map((column) => {
              const direction =
                sort?.column === column.id ? sort.direction : null;
              const Marker =
                direction === "asc"
                  ? ArrowUpIcon
                  : direction === "desc"
                    ? ArrowDownIcon
                    : ArrowUpDownIcon;
              return (
                <th
                  key={column.id}
                  scope="col"
                  aria-sort={
                    direction
                      ? direction === "asc"
                        ? "ascending"
                        : "descending"
                      : undefined
                  }
                  className={cn(
                    "sticky top-0 z-10 h-8 bg-card px-3 text-left font-medium whitespace-nowrap text-muted-foreground shadow-[inset_0_-1px_var(--border)]",
                    column.numeric && "text-right",
                  )}
                >
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    className="-mx-2 h-7 gap-1 px-2 text-xs font-medium hover:bg-transparent"
                    disabled={loading}
                    onClick={() => toggle(column)}
                  >
                    {column.header}
                    {column.headerDetail && (
                      <span className="text-[10px] font-normal">
                        {column.headerDetail}
                      </span>
                    )}
                    <Marker
                      aria-hidden="true"
                      className={cn("size-3.5", !direction && "opacity-40")}
                    />
                  </Button>
                </th>
              );
            })}
          </tr>
        </thead>
        <tbody className="[&_tr:last-child]:border-0">
          {loading ? (
            Array.from({ length: 6 }, (_, index) => (
              <tr key={index} aria-hidden="true" className="border-b">
                {columns.map((column, at) => (
                  <td key={column.id} className={cellClass}>
                    <Skeleton
                      className={cn("h-3", at % 2 ? "w-1/2" : "w-3/4")}
                    />
                  </td>
                ))}
              </tr>
            ))
          ) : sorted.length === 0 ? (
            <tr>
              <td
                colSpan={columns.length}
                className="p-4 text-muted-foreground"
              >
                {empty}
              </td>
            </tr>
          ) : (
            sorted.map((row, index) => (
              <tr
                key={rowKey(row, index)}
                tabIndex={onRowActivate ? 0 : undefined}
                aria-label={rowLabel?.(row)}
                className={cn(
                  "border-b transition-colors hover:bg-muted/50",
                  onRowActivate &&
                    "cursor-pointer focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none focus-visible:ring-inset",
                )}
                onClick={(event) => {
                  if (!(event.target as Element).closest("a, button"))
                    onRowActivate?.(row);
                }}
                onKeyDown={(event) => {
                  if (event.target !== event.currentTarget) return;
                  if (event.key !== "Enter" && event.key !== " ") return;
                  event.preventDefault();
                  onRowActivate?.(row);
                }}
              >
                {columns.map((column) => {
                  const Cell = column.rowHeader ? "th" : "td";
                  return (
                    <Cell
                      key={column.id}
                      scope={column.rowHeader ? "row" : undefined}
                      className={cn(
                        cellClass,
                        column.rowHeader && "text-left font-medium",
                        column.numeric && "text-right",
                      )}
                    >
                      {column.cell(row)}
                    </Cell>
                  );
                })}
              </tr>
            ))
          )}
        </tbody>
      </table>
    </div>
  );
}

export function column<T>(
  id: string,
  header: string,
  value: (row: T) => string | number | null | undefined,
  cell: (row: T) => ReactNode = (row) => show(value(row)),
): TableColumn<T> {
  return { id, header, sortValue: value, cell };
}

export const mono = <T,>(
  id: string,
  header: string,
  value: (row: T) => unknown,
) =>
  column<T>(
    id,
    header,
    (row) => show(value(row)),
    (row) => <span className="font-mono">{show(value(row))}</span>,
  );

export const num = <T,>(
  id: string,
  header: string,
  value: (row: T) => number,
  digits = 0,
  headerDetail?: string,
): TableColumn<T> => ({
  ...column(id, header, value, (row) =>
    value(row).toLocaleString("en-US", {
      minimumFractionDigits: digits,
      maximumFractionDigits: digits,
    }),
  ),
  numeric: true,
  headerDetail,
});
