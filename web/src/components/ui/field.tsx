import { Field } from "@ark-ui/solid/field"
import { type JSX, Show } from "solid-js"
import { css } from "styled-system/css"

import { Checkbox } from "./checkbox"

const styles = {
  root: css({ alignContent: "start", display: "grid", gap: "1.5", minWidth: "0" }),
  group: css({
    alignContent: "start",
    border: "none",
    display: "grid",
    gap: "1.5",
    margin: "0",
    minWidth: "0",
    padding: "0",
  }),
  label: css({
    alignItems: "center",
    color: "fg.muted",
    display: "flex",
    fontSize: "xs",
    fontWeight: "semibold",
    gap: "1",
  }),
  hint: css({ color: "fg.muted", fontSize: "xs", lineHeight: "1.5" }),
  error: css({ color: "danger.default", fontSize: "xs", lineHeight: "1.5" }),
  toggle: css({
    alignItems: "center",
    cursor: "pointer",
    display: "inline-flex",
    fontSize: "sm",
    gap: "2.5",
    "&:has(:disabled)": { layerStyle: "disabled" },
  }),
}

export interface FormFieldProps {
  label: string
  hint?: string
  error?: string
  required?: boolean
  children: JSX.Element
}

export const FormField = (props: FormFieldProps) => (
  <Field.Root class={styles.root} required={props.required} invalid={Boolean(props.error)}>
    <Field.Label class={styles.label}>{props.label}</Field.Label>
    {props.children}
    <Show when={props.hint}>
      <Field.HelperText class={styles.hint}>{props.hint}</Field.HelperText>
    </Show>
    <Show when={props.error}>
      <Field.ErrorText class={styles.error}>{props.error}</Field.ErrorText>
    </Show>
  </Field.Root>
)

/**
 * A labelled group of repeated controls, each with its own label, since a
 * Field.Root would give them all one control id.
 */
export const FieldGroup = (props: { label: string; hint?: string; children: JSX.Element }) => (
  <fieldset class={styles.group}>
    <legend class={styles.label}>{props.label}</legend>
    {props.children}
    <Show when={props.hint}>
      <p class={styles.hint}>{props.hint}</p>
    </Show>
  </fieldset>
)

export interface ToggleFieldProps {
  label: JSX.Element
  checked: boolean
  disabled?: boolean
  onChange: (checked: boolean) => void
}

export const ToggleField = (props: ToggleFieldProps) => (
  <label class={styles.toggle}>
    <Checkbox
      checked={props.checked}
      disabled={props.disabled}
      onChange={(event) => props.onChange(event.currentTarget.checked)}
    />
    {props.label}
  </label>
)
