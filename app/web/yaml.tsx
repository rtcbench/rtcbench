import { type ReactNode, useMemo } from "react";
import { CST, Lexer } from "yaml";
import { cn } from "./ui";

const tones = {
  key: "text-link",
  string: "text-mint-600 dark:text-mint-400",
  literal: "text-amber-700 dark:text-amber-300",
  variable: "text-violet-600 dark:text-violet-400",
  muted: "text-muted-foreground",
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
  return <pre className="code-block">{html}</pre>;
}

export function YamlEditor({
  id,
  value,
  onChange,
  className,
}: {
  id: string;
  value: string;
  onChange: (value: string) => void;
  className?: string;
}) {
  const html = useMemo(() => highlight(value), [value]);
  return (
    <div
      className={cn(
        "code-block grid p-0 focus-within:border-ring focus-within:ring-[3px] focus-within:ring-ring/50",
        className,
      )}
    >
      <pre
        aria-hidden="true"
        className="pointer-events-none col-start-1 row-start-1 p-4 break-all whitespace-pre-wrap"
      >
        {html}
        {"\n"}
      </pre>
      <textarea
        id={id}
        spellCheck="false"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className="col-start-1 row-start-1 resize-none overflow-hidden bg-transparent p-4 break-all whitespace-pre-wrap text-transparent caret-foreground outline-none selection:bg-primary/25"
      />
    </div>
  );
}
