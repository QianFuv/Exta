/** Semi controls adapted to Exta's existing string-valued, validated form state. */
import { Children, isValidElement, useId, useRef, useState, useLayoutEffect } from "react";
import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode, Ref } from "react";
import SemiButton from "@douyinfe/semi-ui/lib/es/button";
import SemiInput from "@douyinfe/semi-ui/lib/es/input";
import InputNumber from "@douyinfe/semi-ui/lib/es/inputNumber";
import Checkbox from "@douyinfe/semi-ui/lib/es/checkbox";
import Switch from "@douyinfe/semi-ui/lib/es/switch";
import SemiSelect from "@douyinfe/semi-ui/lib/es/select";
import DatePicker from "@douyinfe/semi-ui/lib/es/datePicker";
import IconCalendar from "@douyinfe/semi-icons/lib/es/icons/IconCalendar";
import IconChevronDown from "@douyinfe/semi-icons/lib/es/icons/IconChevronDown";
import Collapse from "@douyinfe/semi-ui/lib/es/collapse";
import Dropdown from "@douyinfe/semi-ui/lib/es/dropdown";

/** Omit absent native attributes before forwarding to strict component prop types. */
function defined<T extends object>(props: T): { [K in keyof T]: Exclude<T[K], undefined> } {
  return Object.fromEntries(Object.entries(props).filter(([, value]) => value !== undefined)) as {
    [K in keyof T]: Exclude<T[K], undefined>;
  };
}

type Change = { target: { value: string; checked: boolean } };

/** Translate the existing visual intent into official Semi button variants. */
export function Button({
  className = "",
  type = "button",
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { loading?: boolean }) {
  const isPrimary = className.includes("button-primary");
  const isDanger = className.includes("button-danger");
  return (
    <SemiButton
      {...props}
      htmlType={type}
      type={isDanger ? "danger" : isPrimary ? "primary" : "tertiary"}
      theme={
        className.includes("button-ghost")
          ? "borderless"
          : isPrimary || isDanger
            ? "solid"
            : className.includes("button-ghost")
              ? "borderless"
              : "outline"
      }
      size={className.includes("button-small") ? "small" : "default"}
      className={className
        .split(" ")
        .filter((name) => !name.startsWith("button"))
        .join(" ")}
    />
  );
}

type InputProps = Omit<
  InputHTMLAttributes<HTMLInputElement>,
  "onInput" | "onChange" | "size" | "value" | "defaultValue"
> & {
  value?: string | number;
  defaultValue?: string | number;
  onChange?: ((event: Change) => void) | undefined;
  ref?: Ref<HTMLInputElement>;
};

/** Use Semi's actual input, number, password, checkbox and calendar implementations. */
export function Input({ onChange, type, inputMode, ref, ...props }: InputProps) {
  const change = (value: string, checked = false) => onChange?.({ target: { value, checked } });
  const validateStatus = props["aria-invalid"] === true ? "error" : "default";
  if (type === "datetime-local") return <DateInput {...props} onChange={onChange} />;
  if (inputMode === "numeric" || type === "number") {
    const { min, max, step, ...rest } = props;
    return (
      <InputNumber
        {...defined(rest)}
        forwardedRef={(element) => {
          if (typeof ref === "function") ref(element);
          else if (ref) ref.current = element;
        }}
        validateStatus={validateStatus}
        hideButtons={false}
        {...(min === undefined ? {} : { min: Number(min) })}
        {...(max === undefined ? {} : { max: Number(max) })}
        {...(step === undefined ? {} : { step: Number(step) })}
        onChange={(value) => change(String(value))}
      />
    );
  }
  return (
    <SemiInput
      {...defined(props)}
      ref={ref}
      type={type === "search" || !type ? "text" : type}
      {...(inputMode ? { inputMode } : {})}
      {...(type === "password" ? { mode: "password" as const } : {})}
      validateStatus={validateStatus}
      onChange={(value) => change(value)}
    />
  );
}

/** Use option children as declarative data; no native select is rendered. */
export function Select({
  children,
  onChange,
  value,
  id,
  ...props
}: {
  children: ReactNode;
  value: string;
  id?: string;
  disabled?: boolean;
  "aria-label"?: string;
  "aria-describedby"?: string;
  onChange: (event: Change) => void;
}) {
  const [isOpen, setOpen] = useState(false);
  const labelId = useId();
  const container = useRef<HTMLSpanElement>(null);
  useLayoutEffect(() => {
    const trigger = container.current?.querySelector('[role="combobox"]');
    if (!trigger) return;
    /** Semi 2.104 retains an active descendant after its option popup unmounts. */
    const clearCollapsedReference = () => {
      if (
        trigger.getAttribute("aria-expanded") === "false" &&
        trigger.hasAttribute("aria-activedescendant")
      )
        trigger.removeAttribute("aria-activedescendant");
    };
    clearCollapsedReference();
    const observer = new MutationObserver(clearCollapsedReference);
    observer.observe(trigger, {
      attributes: true,
      attributeFilter: ["aria-expanded", "aria-activedescendant"],
    });
    return () => observer.disconnect();
  }, []);

  const options = Children.toArray(children)
    .filter(isValidElement<{ value: string; children: ReactNode; disabled?: boolean }>)
    .map((option) => ({
      value: option.props.value,
      label: option.props.children,
      disabled: option.props.disabled ?? false,
      showTick: false,
    }));
  return (
    <span ref={container} className="exta-select" data-exta-popup-open={isOpen}>
      {props["aria-label"] ? (
        <span id={labelId} className="visually-hidden">
          {props["aria-label"]}
        </span>
      ) : null}
      <SemiSelect
        {...props}
        aria-labelledby={props["aria-label"] ? labelId : id ? id + "-label" : undefined}
        id={id}
        value={value}
        optionList={options}
        arrowIcon={<IconChevronDown aria-hidden="true" />}
        motion={false}
        onDropdownVisibleChange={setOpen}
        onChange={(selected) =>
          onChange({ target: { value: String(selected ?? ""), checked: false } })
        }
      />
    </span>
  );
}

/** A controlled Semi menu returns focus before opening a separate confirmation dialog. */
export function ActionMenu({
  label,
  accessibleLabel = label,
  disabled = false,
  primary = false,
  items,
}: {
  label: string;
  accessibleLabel?: string;
  disabled?: boolean;
  primary?: boolean;
  items: ReadonlyArray<{
    label: string;
    disabled?: boolean;
    danger?: boolean;
    onSelect: () => void;
  }>;
}) {
  const [isOpen, setOpen] = useState(false);
  const trigger = useRef<HTMLSpanElement>(null);
  return (
    <span ref={trigger} data-exta-popup-open={isOpen}>
      <Dropdown
        trigger="custom"
        visible={isOpen}
        motion={false}
        position="bottomRight"
        onClickOutSide={() => setOpen(false)}
        closeOnEsc
        onEscKeyDown={(event) => {
          event.stopPropagation();
          setOpen(false);
          trigger.current?.querySelector("button")?.focus();
        }}
        render={
          <Dropdown.Menu>
            {items.map((item) => (
              <Dropdown.Item
                key={item.label}
                disabled={item.disabled ?? false}
                {...(item.danger ? { type: "danger" as const } : {})}
                onClick={() => {
                  setOpen(false);
                  trigger.current?.querySelector("button")?.focus();
                  item.onSelect();
                }}
              >
                {item.label}
              </Dropdown.Item>
            ))}
          </Dropdown.Menu>
        }
      >
        <SemiButton
          aria-label={accessibleLabel}
          aria-haspopup="menu"
          aria-expanded={isOpen}
          disabled={disabled}
          theme={primary ? "solid" : "borderless"}
          type={primary ? "primary" : "tertiary"}
          onClick={() => setOpen(!isOpen)}
          onKeyDownCapture={(event) => {
            if (event.key === "Enter" || event.key === " ") event.stopPropagation();
          }}
        >
          {label} <IconChevronDown aria-hidden />
        </SemiButton>
      </Dropdown>
    </span>
  );
}

/** Keep typed local dates and calendar selection in the same validated form state. */
function DateInput({ onChange, value, ...props }: InputProps) {
  const [isOpen, setOpen] = useState(false);
  const parsed = value ? new Date(value) : undefined;
  return (
    <span className="exta-date-input" data-exta-popup-open={isOpen}>
      <DatePicker
        type="dateTime"
        format="yyyy-MM-dd HH:mm"
        motion={false}
        open={isOpen}
        onOpenChange={setOpen}
        className="exta-date-picker"
        {...(parsed && !Number.isNaN(parsed.getTime()) ? { value: parsed } : {})}
        disabled={props.disabled ?? false}
        onChange={(_formatted, date) => {
          if (!(date instanceof Date)) return onChange?.({ target: { value: "", checked: false } });
          const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000);
          onChange?.({ target: { value: local.toISOString().slice(0, 16), checked: false } });
        }}
        triggerRender={() => (
          <SemiInput
            {...defined(props)}
            value={String(value ?? "").replace("T", " ")}
            placeholder="YYYY-MM-DD HH:mm"
            suffix={<IconCalendar aria-hidden />}
            validateStatus={props["aria-invalid"] === true ? "error" : "default"}
            onChange={(text) =>
              onChange?.({ target: { value: text.replace(" ", "T"), checked: false } })
            }
            onKeyDown={(event) => {
              if (event.key === "ArrowDown") {
                event.preventDefault();
                setOpen(true);
              }
              if (event.key === "Escape") setOpen(false);
            }}
          />
        )}
      />
    </span>
  );
}

/** A consistent keyboard-operable disclosure for secondary diagnostic text. */
export function Disclosure({ label, children }: { label: string; children: ReactNode }) {
  return (
    <Collapse className="exta-disclosure" motion={false}>
      <Collapse.Panel header={label} itemKey="details">
        {children}
      </Collapse.Panel>
    </Collapse>
  );
}

/** Let Semi associate the caption with its actual input, without nested labels. */
export function CheckboxField({
  label,
  onChange,
  ...props
}: Pick<InputProps, "id" | "checked" | "disabled" | "aria-describedby" | "onChange"> & {
  label: ReactNode;
}) {
  return (
    <Checkbox
      {...defined(props)}
      onChange={(event) =>
        onChange?.({ target: { value: "", checked: event.target.checked ?? false } })
      }
    >
      {label}
    </Checkbox>
  );
}

/** Represent an on/off configuration without conflating it with dispatch readiness. */
export function SwitchField({
  label,
  checked,
  disabled,
  id,
  onChange,
}: {
  label: string;
  checked: boolean;
  disabled?: boolean;
  id?: string;
  onChange: (event: Change) => void;
}) {
  return (
    <span className="switch-field">
      <Switch
        {...defined({ id, disabled })}
        aria-label={label}
        checked={checked}
        onChange={(value) => onChange({ target: { value: "", checked: value } })}
      />
      <span>{label}</span>
    </span>
  );
}
