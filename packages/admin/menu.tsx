import { DropdownMenu } from "radix-ui";

export function MenuContent({
  children,
  className = "w-56",
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <DropdownMenu.Portal>
      <DropdownMenu.Content
        align="start"
        sideOffset={4}
        collisionPadding={8}
        className={
          "z-50 max-h-[var(--radix-dropdown-menu-content-available-height)] overflow-auto rounded-lg border bg-popover p-1 text-popover-foreground shadow-sm " +
          className
        }
      >
        {children}
      </DropdownMenu.Content>
    </DropdownMenu.Portal>
  );
}
