import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { type ClassValue, clsx } from "clsx";
import {
  ArrowLeftIcon,
  CheckIcon,
  ChevronDownIcon,
  CopyIcon,
  Loader2Icon,
} from "lucide-react";
import {
  Checkbox as CheckboxPrimitive,
  Select as SelectPrimitive,
  Slot,
} from "radix-ui";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

const raisedButtonSurface = [
  "border border-border border-b-input bg-card bg-linear-to-b from-card to-muted text-foreground",
  "shadow-[inset_0_1px_0_#ffffff,0_1px_1px_#00000014,0_3px_4px_#0000000d]",
  "hover:to-background hover:shadow-[inset_0_1px_0_#ffffff,0_2px_3px_#0000001a,0_4px_6px_#0000000d]",
  "active:translate-y-px active:from-muted active:to-card active:shadow-[inset_0_1px_2px_#0000001a,0_1px_0_#ffffff80]",
  "dark:border-foreground/15 dark:border-b-foreground/10 dark:bg-secondary dark:from-[color-mix(in_srgb,var(--input)_88%,var(--foreground))] dark:to-input dark:text-popover-foreground",
  "dark:shadow-[inset_0_1px_0_#ffffff14,0_1px_2px_#00000026]",
  "dark:hover:border-foreground/25 dark:hover:from-[color-mix(in_srgb,var(--input)_82%,var(--foreground))] dark:hover:to-input dark:hover:shadow-[inset_0_1px_0_#ffffff1f,0_2px_4px_#00000033]",
  "dark:active:from-input dark:active:to-secondary dark:active:shadow-[inset_0_1px_2px_#00000033,0_1px_0_#ffffff0a]",
  "disabled:bg-secondary disabled:bg-none disabled:shadow-none dark:disabled:shadow-none",
].join(" ");

const buttonVariants = cva(
  "inline-flex shrink-0 items-center justify-center gap-2 rounded-md text-sm font-medium whitespace-nowrap transition-all outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:pointer-events-none disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default:
          "border border-transparent bg-primary text-primary-foreground shadow-xs hover:bg-primary/90 active:translate-y-px",
        destructive:
          "border border-transparent bg-destructive-muted text-destructive hover:border-destructive/20 hover:bg-destructive/15 focus-visible:ring-destructive/20 dark:focus-visible:ring-destructive/40",
        secondary: raisedButtonSurface,
        ghost:
          "hover:bg-accent hover:text-accent-foreground dark:hover:bg-accent/50",
        link: "text-link underline-offset-4 hover:underline",
      },
      size: {
        default: "h-(--control-height) px-4 py-2 has-[>svg]:px-3",
        sm: "h-(--control-height-sm) gap-1.5 rounded-md px-3 has-[>svg]:px-2.5",
        "icon-xs": "size-6 rounded-md [&_svg:not([class*='size-'])]:size-3",
        "icon-sm": "size-(--control-height-sm)",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  },
);

function Button({
  className,
  variant = "default",
  size = "default",
  asChild = false,
  ...props
}: React.ComponentProps<"button"> &
  VariantProps<typeof buttonVariants> & {
    asChild?: boolean;
  }) {
  const Comp = asChild ? Slot.Root : "button";

  return (
    <Comp
      data-slot="button"
      data-variant={variant}
      data-size={size}
      className={cn(buttonVariants({ variant, size, className }))}
      {...props}
    />
  );
}

export { Button, buttonVariants };

const badgeVariants = cva(
  "inline-flex w-fit shrink-0 items-center justify-center gap-1 overflow-hidden rounded-full border border-transparent px-2 py-0.5 text-xs font-medium whitespace-nowrap transition-[color,box-shadow] focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 aria-invalid:border-destructive aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 [&>svg]:pointer-events-none [&>svg]:size-3",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground [a&]:hover:bg-primary/90",
        destructive:
          "border-destructive/30 bg-destructive/15 text-destructive focus-visible:ring-destructive/20 [a&]:hover:bg-destructive/25",
        success:
          "border-success/30 bg-success/15 text-success [a&]:hover:bg-success/25",
        warning:
          "border-warning/30 bg-warning/15 text-warning [a&]:hover:bg-warning/25",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  },
);

function Badge({
  className,
  variant = "default",
  asChild = false,
  ...props
}: React.ComponentProps<"span"> &
  VariantProps<typeof badgeVariants> & { asChild?: boolean }) {
  const Comp = asChild ? Slot.Root : "span";

  return (
    <Comp
      data-slot="badge"
      data-variant={variant}
      className={cn(badgeVariants({ variant }), className)}
      {...props}
    />
  );
}

export { Badge };

const alertVariants = cva(
  "relative grid w-full grid-cols-[0_1fr] items-start gap-y-0.5 rounded-lg border px-4 py-3 text-sm has-[>svg]:grid-cols-[calc(var(--spacing)*4)_1fr] has-[>svg]:gap-x-3 [&>svg]:size-4 [&>svg]:translate-y-0.5 [&>svg]:text-current",
  {
    variants: {
      variant: {
        destructive:
          "border-destructive/40 bg-card text-destructive *:data-[slot=alert-description]:text-destructive/90 [&>svg]:text-current",
        warning:
          "border-warning/40 bg-card text-foreground [&>svg]:text-warning",
      },
    },
  },
);

function Alert({
  className,
  variant,
  ...props
}: React.ComponentProps<"div"> & VariantProps<typeof alertVariants>) {
  return (
    <div
      data-slot="alert"
      role="alert"
      className={cn(alertVariants({ variant }), className)}
      {...props}
    />
  );
}

function AlertTitle({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="alert-title"
      className={cn(
        "col-start-2 line-clamp-1 min-h-4 font-medium tracking-tight",
        className,
      )}
      {...props}
    />
  );
}

function AlertDescription({
  className,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="alert-description"
      className={cn(
        "col-start-2 grid justify-items-start gap-1 text-sm text-muted-foreground [&_p]:leading-relaxed",
        className,
      )}
      {...props}
    />
  );
}

export { Alert, AlertTitle, AlertDescription };

function Input({
  className,
  type,
  size = "default",
  ...props
}: Omit<React.ComponentProps<"input">, "size"> & {
  size?: "sm" | "default";
}) {
  return (
    <input
      type={type}
      autoComplete="off"
      data-slot="input"
      data-size={size}
      className={cn(
        "h-(--control-height) w-full min-w-0 rounded-md border border-input bg-input-background px-(--control-px) py-1 text-base shadow-xs transition-[color,box-shadow] outline-none selection:bg-primary selection:text-primary-foreground file:me-3 file:inline-flex file:h-7 file:border-0 file:bg-transparent file:text-sm file:font-medium file:text-foreground placeholder:text-muted-foreground disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 md:text-sm",
        "data-[size=sm]:h-(--control-height-sm) data-[size=sm]:px-2 data-[size=sm]:text-xs",
        "focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50",
        "aria-invalid:border-destructive aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40",
        "[&::-webkit-search-cancel-button]:appearance-none [&::-webkit-search-decoration]:appearance-none [&::-webkit-search-results-button]:appearance-none",
        "[&::-webkit-calendar-picker-indicator]:opacity-60 [&::-webkit-datetime-edit]:leading-none",
        className,
      )}
      {...props}
    />
  );
}

export { Input };

function Checkbox({
  className,
  ...props
}: React.ComponentProps<typeof CheckboxPrimitive.Root>) {
  return (
    <CheckboxPrimitive.Root
      data-slot="checkbox"
      className={cn(
        "peer size-4 shrink-0 rounded-[4px] border border-input shadow-xs transition-shadow outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-destructive/20 data-[state=checked]:border-primary data-[state=checked]:bg-primary data-[state=checked]:text-primary-foreground dark:bg-input/30 dark:aria-invalid:ring-destructive/40 dark:data-[state=checked]:bg-primary",
        className,
      )}
      {...props}
    >
      <CheckboxPrimitive.Indicator
        data-slot="checkbox-indicator"
        className="grid place-content-center text-current transition-none"
      >
        <CheckIcon className="size-3.5" />
      </CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  );
}

export { Checkbox };

function Field({
  className,
  orientation,
  ...props
}: React.ComponentProps<"div"> & { orientation?: "horizontal" }) {
  return (
    <div
      role="group"
      className={cn(
        "flex w-full gap-3 data-[invalid=true]:text-destructive",
        orientation ? "items-center" : "flex-col",
        className,
      )}
      {...props}
    />
  );
}

function FieldLabel({ className, ...props }: React.ComponentProps<"label">) {
  return (
    <label
      className={cn(
        "flex w-fit items-center gap-2 text-sm leading-snug font-medium select-none",
        className,
      )}
      {...props}
    />
  );
}

function FieldError({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      role="alert"
      className={cn("text-sm text-destructive", className)}
      {...props}
    />
  );
}

export { Field, FieldLabel, FieldError };

function Select(props: React.ComponentProps<typeof SelectPrimitive.Root>) {
  return <SelectPrimitive.Root data-slot="select" {...props} />;
}

function SelectValue(
  props: React.ComponentProps<typeof SelectPrimitive.Value>,
) {
  return <SelectPrimitive.Value data-slot="select-value" {...props} />;
}

function SelectTrigger({
  className,
  size = "default",
  children,
  ...props
}: React.ComponentProps<typeof SelectPrimitive.Trigger> & {
  size?: "sm" | "default";
}) {
  return (
    <SelectPrimitive.Trigger
      data-slot="select-trigger"
      data-size={size}
      className={cn(
        "flex h-(--control-height) w-full min-w-0 items-center justify-between gap-2 rounded-md border border-input bg-input-background px-(--control-px) text-sm whitespace-nowrap shadow-xs transition-[color,box-shadow] outline-none data-[placeholder]:text-muted-foreground",
        "data-[size=sm]:h-(--control-height-sm) data-[size=sm]:px-2 data-[size=sm]:text-xs",
        "focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50",
        "aria-invalid:border-destructive aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40",
        "disabled:cursor-not-allowed disabled:opacity-50",
        "*:data-[slot=select-value]:truncate [&_svg]:pointer-events-none [&_svg]:shrink-0",
        className,
      )}
      {...props}
    >
      {children}
      <SelectPrimitive.Icon asChild>
        <ChevronDownIcon className="size-4 text-muted-foreground" />
      </SelectPrimitive.Icon>
    </SelectPrimitive.Trigger>
  );
}

function SelectContent({
  className,
  children,
  ...props
}: React.ComponentProps<typeof SelectPrimitive.Content>) {
  return (
    <SelectPrimitive.Portal>
      <SelectPrimitive.Content
        data-slot="select-content"
        position="popper"
        sideOffset={4}
        className={cn(
          "relative z-50 max-h-(--radix-select-content-available-height) min-w-(--radix-select-trigger-width) overflow-x-hidden overflow-y-auto rounded-lg border bg-popover/90 p-1 text-popover-foreground shadow-lg backdrop-blur-xl backdrop-saturate-150",
          className,
        )}
        {...props}
      >
        <SelectPrimitive.Viewport>{children}</SelectPrimitive.Viewport>
      </SelectPrimitive.Content>
    </SelectPrimitive.Portal>
  );
}

function SelectItem({
  className,
  children,
  ...props
}: React.ComponentProps<typeof SelectPrimitive.Item>) {
  return (
    <SelectPrimitive.Item
      data-slot="select-item"
      className={cn(
        "relative flex w-full cursor-default items-center rounded-md py-1.5 pr-8 pl-2 text-sm outline-none select-none data-[disabled]:pointer-events-none data-[disabled]:opacity-50 data-[highlighted]:bg-accent data-[highlighted]:text-accent-foreground",
        className,
      )}
      {...props}
    >
      <SelectPrimitive.ItemText>{children}</SelectPrimitive.ItemText>
      <SelectPrimitive.ItemIndicator className="absolute right-2 flex items-center">
        <CheckIcon className="size-4 text-primary" />
      </SelectPrimitive.ItemIndicator>
    </SelectPrimitive.Item>
  );
}

export { Select, SelectContent, SelectItem, SelectTrigger, SelectValue };

const bars = [
  { width: 23, color: "#dc3545" },
  { width: 17, color: "#2b78e4" },
  { width: 28, color: "#f5c518" },
  { width: 13, color: "#f28c28" },
  { width: 25.5, color: "#2ab673" },
];

function Logo({ className, ...props }: React.ComponentProps<"svg">) {
  return (
    <svg
      data-slot="logo"
      viewBox="0 0 32 32"
      aria-hidden="true"
      className={cn("size-8 shrink-0", className)}
      {...props}
    >
      {bars.map((bar, i) => (
        <rect
          key={bar.color}
          x="2"
          y={2 + i * 6}
          width={bar.width}
          height="4"
          rx="1"
          fill={bar.color}
        />
      ))}
    </svg>
  );
}

function Wordmark({
  className,
  children = "RTCBench",
  ...props
}: React.ComponentProps<"span">) {
  return (
    <span
      data-slot="wordmark"
      className={cn("font-display font-bold tracking-tight", className)}
      {...props}
    >
      {children}
    </span>
  );
}

export { Logo, Wordmark };

function Spinner({ className, ...props }: React.ComponentProps<"svg">) {
  return (
    <Loader2Icon
      role="status"
      aria-label="Loading"
      className={cn("size-4 animate-spin", className)}
      {...props}
    />
  );
}

export { Spinner };

function Skeleton({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="skeleton"
      className={cn("animate-pulse rounded-md bg-accent", className)}
      {...props}
    />
  );
}

export { Skeleton };

export function CopyButton({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = React.useState(false);
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-xs"
      aria-label={label}
      title={label}
      onClick={() =>
        navigator.clipboard.writeText(value).then(() => setCopied(true))
      }
    >
      {copied ? <CheckIcon /> : <CopyIcon />}
    </Button>
  );
}

export function Back({ href, label }: { href: string; label: string }) {
  return (
    <Button variant="ghost" size="sm" className="-ml-2.5 self-start" asChild>
      <a href={href}>
        <ArrowLeftIcon />
        {label}
      </a>
    </Button>
  );
}
