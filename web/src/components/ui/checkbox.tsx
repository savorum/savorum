import { type ComponentProps, splitProps } from "solid-js"
import { cva, cx } from "styled-system/css"

const checkbox = cva({
  base: {
    accentColor: "accent.default",
    cursor: "pointer",
    flex: "0 0 auto",
    margin: "0",
    _disabled: { cursor: "default" },
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

/** A native checkbox in the accent colour. The label around it supplies the touch target. */
export const Checkbox = (props: CheckboxProps) => {
  const [local, rest] = splitProps(props, ["size", "class"])
  return <input {...rest} type="checkbox" class={cx(checkbox({ size: local.size }), local.class)} />
}
