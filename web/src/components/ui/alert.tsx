import { ark } from "@ark-ui/solid/factory"
import { CircleAlert, CircleCheck, Info, OctagonAlert, TriangleAlert } from "lucide-solid"
import { type ComponentProps, splitProps } from "solid-js"
import { Dynamic } from "solid-js/web"
import { createSlotRecipeContext } from "styled-system/jsx"
import { alert } from "styled-system/recipes"

const { withProvider, withContext } = createSlotRecipeContext(alert)

export type RootProps = ComponentProps<typeof Root>
export const Root = withProvider(ark.div, "root")
export const Title = withContext(ark.h3, "title")
export const Description = withContext(ark.div, "description")
export const Content = withContext(ark.div, "content")

const IndicatorRoot = withContext(ark.div, "indicator")

/** Status icons, so a panel's status does not depend on colour. */
const icons = {
  info: Info,
  warning: TriangleAlert,
  success: CircleCheck,
  error: OctagonAlert,
  neutral: CircleAlert,
}

export type AlertStatus = keyof typeof icons

export interface IndicatorProps extends Omit<ComponentProps<typeof IndicatorRoot>, "children"> {
  status?: AlertStatus
}

export const Indicator = (props: IndicatorProps) => {
  const [local, rest] = splitProps(props, ["status"])
  return (
    <IndicatorRoot {...rest} aria-hidden="true">
      <Dynamic component={icons[local.status ?? "info"]} />
    </IndicatorRoot>
  )
}
