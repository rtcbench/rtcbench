import { useRef, type ReactNode } from "react";
import { PlusIcon, ChevronRightIcon } from "lucide-react";
import { DropdownMenu } from "radix-ui";
import { useWorkspaceIds, useTabSize } from "./context";
export type TabOption = {
  id: string;
  label: string;
  group?: string;
  icon?: ReactNode;
};
import { WorkspaceTabs, type WorkspaceTabsProps } from "./tabs";
import { actionClass, menuItem } from "./controls";

export function WorkspaceToolbar({
  active,
  menuOpen,
  setMenuOpen,
  leading,
  options,
  onOpen,
  ...tabProps
}: Omit<WorkspaceTabsProps, "active" | "dragBoundary"> & {
  leading?: ReactNode;
  options: readonly TabOption[];
  onOpen: (id: string) => string;
  active?: string;
  menuOpen: boolean;
  setMenuOpen: (open: boolean) => void;
}) {
  const ids = useWorkspaceIds();
  const { height } = useTabSize();
  const headerRef = useRef<HTMLDivElement>(null);
  const openedTab = useRef<string | undefined>(undefined);
  const menuOption = (option: TabOption) => (
    <DropdownMenu.Item
      key={option.id}
      className={menuItem}
      onSelect={() => {
        openedTab.current = onOpen(option.id);
      }}
    >
      {option.icon}
      {option.label}
    </DropdownMenu.Item>
  );
  const groups = [
    ...new Set(
      options
        .map((option) => option.group)
        .filter((group): group is string => !!group),
    ),
  ];
  return (
    <div
      ref={headerRef}
      style={{ height }}
      className={
        "lab:relative lab:z-30 lab:flex lab:shrink-0 lab:select-none lab:items-center lab:gap-1 " +
        (leading ? "lab:pl-px lab:sm:pl-[9px]" : "lab:pl-4")
      }
    >
      {leading}
      <div className="lab:min-w-0">
        <WorkspaceTabs
          {...tabProps}
          dragBoundary={headerRef}
          active={active ?? ""}
        />
      </div>
      <div className="lab:flex lab:shrink-0 lab:items-center lab:gap-1">
        {options.length > 0 && (
          <DropdownMenu.Root
            modal={false}
            open={menuOpen}
            onOpenChange={setMenuOpen}
          >
            <DropdownMenu.Trigger asChild>
              <button
                type="button"
                className={actionClass}
                style={{
                  width: Math.min(32, height),
                  height: Math.min(32, height),
                }}
                aria-label="Open a tab"
                title="Open a tab"
              >
                <PlusIcon aria-hidden="true" className="lab:size-4" />
              </button>
            </DropdownMenu.Trigger>
            <DropdownMenu.Portal>
              <DropdownMenu.Content
                align="start"
                sideOffset={4}
                collisionPadding={8}
                className={menuContent}
                onInteractOutside={(event) => {
                  // Preact portals do not bubble focus through the parent menu
                  if ((event.target as Element).closest("[data-tab-submenu]"))
                    event.preventDefault();
                }}
                onCloseAutoFocus={(event) => {
                  if (!openedTab.current) return;
                  event.preventDefault();
                  const key = openedTab.current;
                  openedTab.current = undefined;
                  requestAnimationFrame(() =>
                    document.getElementById(ids.tab(key))?.focus(),
                  );
                }}
              >
                {options.filter((option) => !option.group).map(menuOption)}
                {groups.map((group) => (
                  <DropdownMenu.Sub key={group}>
                    <DropdownMenu.SubTrigger
                      className={
                        menuItem +
                        " lab:justify-between lab:data-[state=open]:bg-accent"
                      }
                    >
                      {group}
                      <ChevronRightIcon
                        aria-hidden="true"
                        className="lab:size-4"
                      />
                    </DropdownMenu.SubTrigger>
                    <DropdownMenu.Portal>
                      <DropdownMenu.SubContent
                        data-tab-submenu
                        className={menuContent}
                        sideOffset={4}
                        collisionPadding={8}
                      >
                        {options
                          .filter((option) => option.group === group)
                          .map(menuOption)}
                      </DropdownMenu.SubContent>
                    </DropdownMenu.Portal>
                  </DropdownMenu.Sub>
                ))}
              </DropdownMenu.Content>
            </DropdownMenu.Portal>
          </DropdownMenu.Root>
        )}
        {tabProps.items.find((tab) => tab.key === active)?.actions}
      </div>
    </div>
  );
}

const menuContent =
  "lab:z-50 lab:min-w-40 lab:max-h-[var(--radix-dropdown-menu-content-available-height)] lab:overflow-auto lab:rounded-lg lab:border lab:border-border lab:bg-popover lab:p-1 lab:font-sans lab:text-sm lab:leading-normal lab:text-popover-foreground lab:shadow-sm";
