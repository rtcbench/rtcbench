import { ContextMenu } from "radix-ui";
import type { ComponentProps, ReactElement } from "react";
import { menuItem } from "./controls";

type Action = readonly [
  label: string,
  onSelect: () => void,
  disabled?: boolean,
];

export function ActionMenu({
  children,
  actions,
  onCloseAutoFocus,
}: {
  children: ReactElement;
  actions: (Action | null | false)[];
  onCloseAutoFocus?: ComponentProps<
    typeof ContextMenu.Content
  >["onCloseAutoFocus"];
}) {
  return (
    <ContextMenu.Root modal={false}>
      <ContextMenu.Trigger asChild>{children}</ContextMenu.Trigger>
      <ContextMenu.Portal>
        <ContextMenu.Content
          className="lab:z-50 lab:font-sans lab:text-sm lab:leading-normal lab:min-w-44 lab:rounded-lg lab:border lab:border-border lab:bg-popover lab:p-1 lab:text-popover-foreground lab:shadow-sm"
          onCloseAutoFocus={onCloseAutoFocus}
        >
          {actions
            .filter((action) => action !== false)
            .map((action, index) =>
              action ? (
                <ContextMenu.Item
                  key={action[0]}
                  className={menuItem}
                  disabled={action[2]}
                  onSelect={action[1]}
                >
                  {action[0]}
                </ContextMenu.Item>
              ) : (
                <ContextMenu.Separator
                  key={index}
                  className="lab:my-1 lab:border-t lab:border-border"
                />
              ),
            )}
        </ContextMenu.Content>
      </ContextMenu.Portal>
    </ContextMenu.Root>
  );
}
