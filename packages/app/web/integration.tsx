import {
  createContext,
  useCallback,
  useContext,
  useLayoutEffect,
  useMemo,
  useRef,
  type ComponentType,
  type ComponentProps,
  type ReactNode,
} from "react";
import { Back as BackButton } from "@rtcbench/admin";

export type PageOptions = {
  apiUrl?: string;
  fetch?: typeof fetch;
  href?: (path: string) => string;
  navigate?: (path: string) => void;
  onDirtyChange?: (dirty: boolean) => void;
};

const Settings = createContext<PageOptions>({});
export const PageTab = createContext<{
  open: (path: string) => void;
  title: (title: string) => void;
  refresh?: (handler?: () => void) => void;
  selected: boolean;
} | null>(null);
const Dirty = createContext<
  ((source: symbol, dirty: boolean) => void) | undefined
>(undefined);
export const useSettings = () => useContext(Settings);
const defaultHref = (path: string) => "/" + path.replace(/^\//, "");

export function PageScope({
  children,
  ...options
}: PageOptions & { children: ReactNode }) {
  const sources = useRef(new Set<symbol>());
  const report = useCallback(
    (source: symbol, dirty: boolean) => {
      if (dirty) sources.current.add(source);
      else sources.current.delete(source);
      options.onDirtyChange?.(sources.current.size > 0);
    },
    [options.onDirtyChange],
  );
  return (
    <Settings.Provider value={options}>
      <Dirty.Provider value={report}>{children}</Dirty.Provider>
    </Settings.Provider>
  );
}

export function page<P extends object>(Content: ComponentType<P>) {
  return function EmbeddedPage({
    apiUrl,
    fetch,
    href,
    navigate,
    onDirtyChange,
    ...props
  }: P & PageOptions) {
    return (
      <PageScope {...{ apiUrl, fetch, href, navigate, onDirtyChange }}>
        <Content {...(props as P)} />
      </PageScope>
    );
  };
}

export function useUnsavedChanges(dirty: boolean) {
  const report = useContext(Dirty);
  const source = useRef(Symbol());
  useLayoutEffect(() => {
    report?.(source.current, dirty);
    return () => report?.(source.current, false);
  }, [dirty, report]);
}

export function usePageTitle(title?: string) {
  const report = useContext(PageTab)?.title;
  useLayoutEffect(() => {
    if (title) report?.(title);
  }, [title, report]);
}

export function useNavigation() {
  const { href = defaultHref, navigate } = useSettings();
  const tab = useContext(PageTab);
  const go = (path: string) => {
    path = path.replace(/^\//, "");
    if (navigate) navigate(path);
    else window.location.assign(href(path));
  };
  return useMemo(
    () => ({
      href,
      go,
      open: tab?.open ?? go,
      link: (path?: string, open = false): ComponentProps<"a"> => ({
        href: path ? href(path.replace(/^\//, "")) : undefined,
        onClick: (event) => {
          if (
            !navigate ||
            !path ||
            event.defaultPrevented ||
            event.button ||
            event.metaKey ||
            event.ctrlKey ||
            event.shiftKey ||
            event.altKey ||
            event.currentTarget.target ||
            event.currentTarget.hasAttribute("download")
          )
            return;
          event.preventDefault();
          if (open && tab) tab.open(path.replace(/^\//, ""));
          else navigate(path.replace(/^\//, ""));
        },
      }),
    }),
    [href, navigate, tab],
  );
}

export function Link({ href, ...props }: ComponentProps<"a">) {
  return <a {...useNavigation().link(href, true)} {...props} />;
}

export function Back({
  href,
  nested,
  onClick,
  ...props
}: ComponentProps<typeof BackButton> & { nested?: boolean }) {
  const tab = useContext(PageTab);
  const link = useNavigation().link(href);
  if (tab && !nested) return null;
  return (
    <BackButton
      {...link}
      {...props}
      onClick={(event) => {
        onClick?.(event);
        if (!event.defaultPrevented) link.onClick?.(event);
      }}
    />
  );
}
