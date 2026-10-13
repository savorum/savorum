import { Clipboard as ArkClipboard } from "@ark-ui/solid/clipboard"
import { Check, Copy } from "lucide-solid"
import { css } from "styled-system/css"
import { styled } from "styled-system/jsx"
import { input } from "styled-system/recipes"
import type { ButtonVariantProps } from "styled-system/recipes/button"

import { Button } from "./button"

const ClipboardInput = styled(ArkClipboard.Input, input)

const styles = {
  root: css({ display: "grid", gap: "1.5", minWidth: "0" }),
  label: css({
    alignItems: "center",
    color: "fg.muted",
    display: "flex",
    fontSize: "xs",
    fontWeight: "semibold",
    gap: "1",
  }),
  control: css({ display: "flex", flexWrap: "wrap", gap: "2", minWidth: "0" }),
  field: css({ flex: "1", minWidth: "16rem" }),
  indicator: css({ alignItems: "center", display: "inline-flex", gap: "2" }),
}

export interface CopyFieldProps {
  label: string
  value: string
  copyLabel: string
  copiedLabel: string
}

/**
 * A secret shown once. The value stays readable and selectable so a copy that
 * the browser refuses is still recoverable by hand.
 */
export const CopyField = (props: CopyFieldProps) => (
  <ArkClipboard.Root class={styles.root} value={props.value} timeout={4000}>
    <ArkClipboard.Label class={styles.label}>{props.label}</ArkClipboard.Label>
    <div class={styles.control}>
      <ClipboardInput
        class={styles.field}
        readOnly
        onFocus={(event) => event.currentTarget.select()}
      />
      <ArkClipboard.Trigger
        asChild={(triggerProps) => (
          <Button {...triggerProps()} variant="outline">
            <ArkClipboard.Indicator
              class={styles.indicator}
              copied={
                <>
                  <Check aria-hidden="true" size={16} />
                  {props.copiedLabel}
                </>
              }
            >
              <Copy aria-hidden="true" size={16} />
              {props.copyLabel}
            </ArkClipboard.Indicator>
          </Button>
        )}
      />
    </div>
  </ArkClipboard.Root>
)

export interface CopyButtonProps {
  value: string
  copyLabel: string
  copiedLabel: string
  size?: ButtonVariantProps["size"]
}

/** Copies a value that is displayed elsewhere, such as a block of codes. */
export const CopyButton = (props: CopyButtonProps) => (
  <ArkClipboard.Root value={props.value} timeout={4000}>
    <ArkClipboard.Trigger
      asChild={(triggerProps) => (
        <Button {...triggerProps()} variant="outline" size={props.size}>
          <ArkClipboard.Indicator
            class={styles.indicator}
            copied={
              <>
                <Check aria-hidden="true" size={16} />
                {props.copiedLabel}
              </>
            }
          >
            <Copy aria-hidden="true" size={16} />
            {props.copyLabel}
          </ArkClipboard.Indicator>
        </Button>
      )}
    />
  </ArkClipboard.Root>
)
