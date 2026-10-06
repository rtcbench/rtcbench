import * as React from "react";
import { CircleAlertIcon, XIcon } from "lucide-react";
import { Dialog as DialogPrimitive } from "radix-ui";
import { Button, cn, CopyButton, Input, Spinner } from "./ui";

function Dialog({
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Root>) {
  return <DialogPrimitive.Root data-slot="dialog" {...props} />;
}

function DialogTrigger({
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Trigger>) {
  return <DialogPrimitive.Trigger data-slot="dialog-trigger" {...props} />;
}

function DialogPortal({
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Portal>) {
  return <DialogPrimitive.Portal data-slot="dialog-portal" {...props} />;
}

function DialogClose({
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Close>) {
  return <DialogPrimitive.Close data-slot="dialog-close" {...props} />;
}

function DialogOverlay({
  className,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Overlay>) {
  return (
    <DialogPrimitive.Overlay
      data-slot="dialog-overlay"
      className={cn("fixed inset-0 z-50 bg-overlay", className)}
      {...props}
    />
  );
}

function DialogContent({
  className,
  children,
  showCloseButton = true,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Content> & {
  showCloseButton?: boolean;
}) {
  return (
    <DialogPortal data-slot="dialog-portal">
      <DialogOverlay />
      <DialogPrimitive.Content
        data-slot="dialog-content"
        className={cn(
          "fixed top-1/2 left-1/2 z-50 grid max-h-[calc(100dvh-2rem)] w-full max-w-[calc(100%-2rem)] -translate-x-1/2 -translate-y-1/2 gap-6 overflow-y-auto rounded-(--radius-dialog) border bg-popover p-6 text-popover-foreground shadow-sm outline-none sm:max-w-lg",
          className,
        )}
        {...props}
      >
        {children}
        {showCloseButton && (
          <DialogPrimitive.Close
            data-slot="dialog-close"
            className="absolute top-4 right-4 inline-flex size-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none disabled:pointer-events-none [&_svg]:pointer-events-none [&_svg]:size-4 [&_svg]:shrink-0"
          >
            <XIcon />
            <span className="sr-only">Close</span>
          </DialogPrimitive.Close>
        )}
      </DialogPrimitive.Content>
    </DialogPortal>
  );
}

function DialogHeader({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="dialog-header"
      className={cn("flex min-w-0 flex-col gap-2 text-left", className)}
      {...props}
    />
  );
}

function DialogFooter({
  className,
  showCloseButton = false,
  children,
  ...props
}: React.ComponentProps<"div"> & {
  showCloseButton?: boolean;
}) {
  return (
    <div
      data-slot="dialog-footer"
      className={cn("flex flex-wrap items-center justify-end gap-2", className)}
      {...props}
    >
      {children}
      {showCloseButton && (
        <DialogPrimitive.Close asChild>
          <Button variant="secondary">Close</Button>
        </DialogPrimitive.Close>
      )}
    </div>
  );
}

function DialogTitle({
  className,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Title>) {
  return (
    <DialogPrimitive.Title
      data-slot="dialog-title"
      className={cn(
        "pr-8 text-lg leading-7 font-semibold break-words",
        className,
      )}
      {...props}
    />
  );
}

export {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
};

export function ConfirmDialog({
  trigger,
  title,
  name,
  confirmLabel,
  typeToConfirm,
  onConfirm,
}: {
  trigger: React.ReactElement;
  title: string;
  name: string;
  confirmLabel: string;
  typeToConfirm?: boolean;
  onConfirm: () => Promise<void>;
}) {
  const [open, setOpen] = React.useState(false);
  const [value, setValue] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState<string>();

  function change(next: boolean) {
    if (busy) return;
    setOpen(next);
    setValue("");
    setError(undefined);
  }

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(undefined);
    try {
      await onConfirm();
      setOpen(false);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={change}>
      <DialogTrigger asChild>{trigger}</DialogTrigger>
      <DialogContent
        role="alertdialog"
        aria-describedby={undefined}
        className="p-0"
        showCloseButton={!busy}
        onOpenAutoFocus={(event) => {
          const input = (event.target as HTMLElement).querySelector("input");
          if (!input) return;
          event.preventDefault();
          input.focus();
        }}
      >
        <form className="flex min-w-0 flex-col" onSubmit={submit}>
          <div className="flex min-w-0 flex-col gap-4 p-6">
            <div className="flex items-center gap-3">
              <span
                aria-hidden="true"
                className="flex size-9 shrink-0 items-center justify-center rounded-xl bg-destructive-muted text-destructive"
              >
                <CircleAlertIcon className="size-5" />
              </span>
              <DialogTitle>
                {title}{" "}
                <span className="inline-flex items-center gap-1 rounded-md border bg-muted/50 py-0.5 pr-0.5 pl-2 align-middle font-mono text-base break-all">
                  {name}
                  <CopyButton value={name} label="Copy name" />
                </span>
                ?
              </DialogTitle>
            </div>
            {typeToConfirm && (
              <Input
                aria-label="Name"
                autoComplete="off"
                spellCheck="false"
                placeholder={name}
                value={value}
                disabled={busy}
                onChange={(event) => setValue(event.target.value)}
              />
            )}
            {error && (
              <p role="alert" className="text-sm text-destructive">
                {error}
              </p>
            )}
          </div>
          <DialogFooter className="justify-between border-t bg-muted/40 px-6 py-4">
            <Button
              type="button"
              variant="ghost"
              disabled={busy}
              onClick={() => change(false)}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              variant="destructive"
              disabled={busy || (typeToConfirm && value !== name)}
            >
              {busy && <Spinner />}
              {confirmLabel}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
