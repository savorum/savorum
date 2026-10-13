import { css } from "styled-system/css"

/** For icons that point backward or forward in reading order, never for pictures of things. */
export const directionalIcon = css({ _rtl: { transform: "scaleX(-1)" } })
