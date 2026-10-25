/** Common numeric presets and exact input share one validated string draft. */
import Slider from "@douyinfe/semi-ui/lib/es/slider";
import { Input } from "./Controls";
import { INTL_LOCALE, useI18n } from "../lib/i18n";

/** A finite slider range never constrains the separately validated precise input. */
export function NumericControl({
  id,
  value,
  onChange,
  minimum,
  maximum,
  presets,
  disabled = false,
  invalid = false,
  describedBy,
  zeroLabel,
  emptyIsZero = false,
}: {
  id: string;
  value: string;
  onChange: (value: string) => void;
  minimum: number;
  maximum: number;
  presets: readonly number[];
  disabled?: boolean;
  invalid?: boolean;
  describedBy?: string;
  zeroLabel?: string;
  emptyIsZero?: boolean;
}) {
  const { formatNumber } = useI18n();
  const stops = [
    ...new Set([minimum, ...presets.filter((entry) => entry >= minimum && entry <= maximum)]),
  ].sort((first, second) => first - second);
  const sliderMaximum = stops.at(-1) ?? maximum;
  const number = Number(value);
  const isInvalid =
    invalid ||
    (value.trim() === "" && !emptyIsZero) ||
    !Number.isSafeInteger(number) ||
    number < minimum ||
    number > maximum;
  const sliderValue = Number.isFinite(number)
    ? Math.max(minimum, Math.min(sliderMaximum, number))
    : minimum;
  const marks = Object.fromEntries(
    stops.map((stop) => [
      stop,
      new Intl.NumberFormat(INTL_LOCALE, { notation: "compact", maximumFractionDigits: 1 }).format(
        stop,
      ),
    ]),
  );
  return (
    <div className="numeric-control">
      <Slider
        min={minimum}
        max={sliderMaximum}
        step={1}
        marks={marks}
        value={sliderValue}
        disabled={disabled}
        aria-labelledby={`${id}-label`}
        aria-valuetext={number === 0 && zeroLabel ? zeroLabel : formatNumber(number)}
        onChange={(next) => {
          if (typeof next === "number") onChange(String(next));
        }}
      />
      <Input
        id={id}
        type="number"
        step={1}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        disabled={disabled}
        aria-invalid={isInvalid}
        aria-describedby={describedBy}
      />
    </div>
  );
}
