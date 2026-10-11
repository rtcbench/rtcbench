import { AlertDialog } from "radix-ui";
import { actionClass } from "./controls";

export function CloseConfirmation({
  label,
  onCancel,
  onConfirm,
  restoreFocus,
}: {
  label?: string;
  onCancel: () => void;
  onConfirm: () => void;
  restoreFocus: () => void;
}) {
  return (
    <AlertDialog.Root
      open={label !== undefined}
      onOpenChange={(open) => !open && onCancel()}
    >
      <AlertDialog.Portal>
        <AlertDialog.Overlay className="lab:fixed lab:inset-0 lab:z-50 lab:font-sans lab:text-sm lab:leading-normal lab:bg-overlay" />
        <AlertDialog.Content
          aria-describedby={undefined}
          className="lab:fixed lab:top-1/2 lab:left-1/2 lab:z-50 lab:font-sans lab:text-sm lab:leading-normal lab:w-[calc(100%-2rem)] lab:max-w-lg lab:-translate-x-1/2 lab:-translate-y-1/2 lab:rounded-[1.25rem] lab:border lab:border-border lab:bg-popover lab:p-6 lab:text-popover-foreground lab:shadow-sm lab:outline-none"
          onCloseAutoFocus={(event) => {
            event.preventDefault();
            restoreFocus();
          }}
        >
          <AlertDialog.Title className="lab:m-0 lab:text-lg lab:font-semibold">
            Discard changes in {label}?
          </AlertDialog.Title>
          <div className="lab:mt-6 lab:flex lab:flex-wrap lab:justify-end lab:gap-2">
            <AlertDialog.Cancel
              className={
                actionClass + " lab:border lab:border-border lab:px-4 lab:py-2"
              }
            >
              Keep tab
            </AlertDialog.Cancel>
            <AlertDialog.Action
              className={
                actionClass +
                " lab:bg-destructive-muted lab:px-4 lab:py-2 lab:text-destructive"
              }
              onClick={onConfirm}
            >
              Discard and close
            </AlertDialog.Action>
          </div>
        </AlertDialog.Content>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  );
}
