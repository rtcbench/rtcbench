import { useId, type ReactNode } from "react";
import { PlusIcon } from "lucide-react";
import {
  Button,
  Field,
  FieldLabel,
  Input,
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "./primitives";

type TextProps = {
  label: string;
  value: string;
  onChange: (value: string) => void;
} & Omit<React.ComponentProps<typeof Input>, "value" | "onChange">;

export function TextField({ label, value, onChange, ...props }: TextProps) {
  const id = useId();
  return (
    <Field data-invalid={props["aria-invalid"] || undefined}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input
        id={id}
        autoComplete="off"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        {...props}
      />
    </Field>
  );
}

export function SelectField({
  label,
  value,
  onChange,
  options,
  children,
}: Pick<TextProps, "label" | "value" | "onChange"> & {
  options: { id: string; name: string; disabled?: boolean }[];
  children?: ReactNode;
}) {
  const id = useId();
  return (
    <Field>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <div className="flex items-center gap-2">
        <Select value={value} onValueChange={onChange}>
          <SelectTrigger id={id} className="flex-1">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {options.map((o) => (
              <SelectItem key={o.id} value={o.id} disabled={o.disabled}>
                {o.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {children}
      </div>
    </Field>
  );
}

export function AddButton({
  children,
  onClick,
}: {
  children: ReactNode;
  onClick: () => void;
}) {
  return (
    <Button type="button" variant="secondary" onClick={onClick}>
      <PlusIcon />
      {children}
    </Button>
  );
}
