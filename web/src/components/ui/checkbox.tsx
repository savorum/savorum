import { type ComponentProps, splitProps } from "solid-js"
import { cva, cx } from "styled-system/css"

const tick = `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16'%3E%3Cpath d='M3.5 8.5l3 3 6-7' fill='none' stroke='black' stroke-width='2.25' stroke-linecap='round' stroke-linejoin='round'/%3E%3C/svg%3E")`

const checkbox = cva({
  base: {
    appearance: "none",
    background: "transparent",
    borderColor: "fg.muted",
    borderRadius: "l1",
    borderWidth: "1.5px",
    cursor: "pointer",
    display: "inline-grid",
    flex: "0 0 auto",
    margin: "0",
    placeItems: "center",
    transitionDuration: "fast",
    transitionProperty: "background-color, border-color",
    _before: {
      background: "accent.solid.fg",
      content: '""',
      height: "100%",
      maskImage: tick,
      maskPosition: "center",
      maskRepeat: "no-repeat",
      maskSize: "85%",
      transform: "scale(0)",
      transitionDuration: "fast",
      transitionProperty: "transform",
      width: "100%",
    },
    _hover: { borderColor: "accent.default" },
    _checked: {
      background: "accent.solid.bg",
      borderColor: "accent.solid.bg",
      _before: { transform: "scale(1)" },
    },
    _disabled: { cursor: "default", opacity: "0.5" },
    _motionReduce: { transitionDuration: "0s", _before: { transitionDuration: "0s" } },
    "@media (forced-colors: active)": { appearance: "auto", _before: { display: "none" } },
  },
  variants: {
    size: {
      md: { height: "1.25rem", width: "1.25rem" },
      lg: { height: "1.5rem", width: "1.5rem" },
    },
  },
  defaultVariants: { size: "md" },
})

type CheckboxProps = Omit<ComponentProps<"input">, "type" | "size"> & { size?: "md" | "lg" }

export const Checkbox = (props: CheckboxProps) => {
  const [local, rest] = splitProps(props, ["size", "class"])
  return <input {...rest} type="checkbox" class={cx(checkbox({ size: local.size }), local.class)} />
}
