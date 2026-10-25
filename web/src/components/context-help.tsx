import { CircleHelp } from "lucide-solid"
import { css } from "styled-system/css"

import { Tooltip } from "@/components/ui/tooltip"
export function ContextHelp(props: { guide: string; label: string }) {
  return (
    <Tooltip
      content={props.label}
      openDelay={250}
      closeDelay={100}
      trigger={(trigger) => (
        <a
          {...trigger()}
          class={style}
          href={`https://docs.savorum.app/guide/${props.guide}/`}
          aria-label={props.label}
          target="_blank"
          rel="noopener noreferrer"
        >
          <CircleHelp aria-hidden="true" size={18} />
        </a>
      )}
    />
  )
}

const style = css({
  alignItems: "center",
  alignSelf: "center",
  borderRadius: "full",
  color: "fg.muted",
  display: "inline-flex",
  flexShrink: "0",
  justifyContent: "center",
  minHeight: "8",
  minWidth: "8",
  textDecoration: "none",
  "@media (pointer: coarse)": { minHeight: "11", minWidth: "11" },
  _hover: { background: "bg.muted", color: "fg.default" },
  _focusVisible: { outline: "2px solid", outlineColor: "accent.default", outlineOffset: "2px" },
})
