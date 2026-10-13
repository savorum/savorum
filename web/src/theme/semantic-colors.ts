import { defineSemanticTokens } from "@pandacss/dev"

export const semanticColors = defineSemanticTokens.colors({
  bg: {
    canvas: { value: "{colors.savorum.canvas}" },
    surface: { value: "{colors.savorum.surface}" },
    subtle: { value: "{colors.savorum.subtle}" },
    muted: { value: "{colors.savorum.muted}" },
  },
  fg: {
    default: { value: "{colors.savorum.charcoal}" },
    muted: { value: "{colors.savorum.charcoal-muted}" },
    subtle: { value: "{colors.savorum.charcoal-muted}" },
  },
  /**
   * Three weights of line, following the steps Radix defines and Park UI's
   * own controls already use: an input rests at `gray.6` and darkens to
   * `gray.7` when it is reached for.
   */
  border: {
    /** Between things: separators, table rules, the line under a heading. */
    subtle: { value: "{colors.gray.6}" },
    /** Around things: a panel, a card, the edge of a menu. */
    default: { value: "{colors.gray.7}" },
    /** The same edge, while it is being reached for. */
    emphasized: { value: "{colors.gray.8}" },
  },
  danger: {
    default: { value: "{colors.savorum.tomato}" },
    muted: { value: "{colors.savorum.tomato-soft}" },
  },
  error: { value: "{colors.savorum.tomato}" },
  /**
   * Something worth saying that is not a refusal: a recipe whose written
   * total disagrees with its own steps is still saveable. Amber rather than
   * the accent, because it is a remark about the recipe and not a control.
   */
  warning: {
    fg: { value: "{colors.savorum.amber}" },
  },
  // Ratings and favourites, deliberately not the accent colour. `default` is
  // what the cook chose, `preview` what hovering offers, `muted` the tint
  // behind it.
  rating: {
    default: { value: "{colors.savorum.amber}" },
    preview: { value: "{colors.savorum.amber-dim}" },
    muted: { value: "{colors.savorum.amber-soft}" },
  },
  favorite: {
    default: { value: "{colors.savorum.tomato}" },
    preview: { value: "{colors.savorum.tomato-dim}" },
    muted: { value: "{colors.savorum.tomato-soft}" },
  },
  recipe: {
    highlight: { value: "{colors.savorum.wheat-soft}" },
  },
})
