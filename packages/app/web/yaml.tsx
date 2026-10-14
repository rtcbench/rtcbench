import { type ReactNode, useMemo } from "react";
import { CST, Lexer } from "yaml";
import { cn } from "@labpane/ui";

const tones = {
  key: "labui:text-link",
  string: "labui:text-success",
  literal: "labui:text-warning",
  variable: "labui:text-violet-600 labui:dark:text-violet-400",
  muted: "labui:text-muted-foreground",
};

const markers = new Set(["\u0002", "\u0018", "\u001f"]);
const literal =
  /^(?:true|false|null|~|[-+]?(?:\d[\d_]*\.?\d*|\.\d+)(?:e[-+]?\d+)?|0x[\da-f]+|[-+]?\.inf|\.nan)$/i;
const variable = /(\$env:\w+(?:=[^\s'"]*)?|\{\{[^}]*\}\})/;

function variables(text: string, tone?: string) {
  return text.split(variable).map((part, i) => (
    <span key={i} className={i % 2 ? tones.variable : tone}>
      {part}
    </span>
  ));
}

function tone(type: string | null) {
  switch (type) {
    case "anchor":
    case "alias":
    case "tag":
      return tones.variable;
    case "space":
    case "newline":
      return undefined;
    default:
      return tones.muted;
  }
}

export function highlight(source: string): ReactNode[] {
  const tokens = [...new Lexer().lex(source)].filter((t) => !markers.has(t));
  const types = tokens.map((t) => CST.tokenType(t));
  return tokens.map((token, i) => {
    const type = types[i]!;
    let next = i + 1;
    while (types[next] === "space") next++;
    const key = types[next] === "map-value-ind";
    if (type === null || type?.endsWith("quoted-scalar")) {
      if (key)
        return (
          <span key={i} className={tones.key}>
            {token}
          </span>
        );
      if (type) return <span key={i}>{variables(token, tones.string)}</span>;
      if (literal.test(token))
        return (
          <span key={i} className={tones.literal}>
            {token}
          </span>
        );
      return <span key={i}>{variables(token)}</span>;
    }
    return (
      <span key={i} className={tone(type)}>
        {token}
      </span>
    );
  });
}

export function YamlView({ text }: { text: string }) {
  const html = useMemo(() => highlight(text), [text]);
  return <pre className="rtc-code-block">{html}</pre>;
}

export function YamlEditor({
  id,
  value,
  onChange,
  className,
  invalid,
  describedBy,
}: {
  id: string;
  value: string;
  onChange: (value: string) => void;
  className?: string;
  invalid?: boolean;
  describedBy?: string;
}) {
  const html = useMemo(() => highlight(value), [value]);
  return (
    <div
      className={cn(
        "rtc-code-block labui:grid labui:p-0 labui:focus-within:border-ring labui:focus-within:ring-[3px] labui:focus-within:ring-ring/50",
        className,
      )}
    >
      <pre
        aria-hidden="true"
        className="labui:pointer-events-none labui:col-start-1 labui:row-start-1 labui:p-4 labui:break-all labui:whitespace-pre-wrap"
      >
        {html}
        {"\n"}
      </pre>
      <textarea
        id={id}
        aria-invalid={invalid}
        aria-describedby={describedBy}
        spellCheck="false"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className="labui:col-start-1 labui:row-start-1 labui:resize-none labui:overflow-hidden labui:bg-transparent labui:p-4 labui:break-all labui:whitespace-pre-wrap labui:text-transparent labui:caret-foreground labui:outline-none labui:selection:bg-primary/25"
      />
    </div>
  );
}
