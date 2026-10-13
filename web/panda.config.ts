import { defineConfig } from "@pandacss/dev"

import { animationStyles } from "@/theme/animation-styles"
import { accent } from "@/theme/colors/accent"
import { amber } from "@/theme/colors/amber"
import { green } from "@/theme/colors/green"
import { red } from "@/theme/colors/red"
import { sand } from "@/theme/colors/sand"
import { conditions } from "@/theme/conditions"
import { globalCss } from "@/theme/global-css"
import { keyframes } from "@/theme/keyframes"
import { layerStyles } from "@/theme/layer-styles"
import { recipes, slotRecipes } from "@/theme/recipes"
import { semanticColors } from "@/theme/semantic-colors"
import { textStyles } from "@/theme/text-styles"
import { colors, savorum } from "@/theme/tokens/colors"
import { durations } from "@/theme/tokens/durations"
import { fonts } from "@/theme/tokens/fonts"
import { shadows } from "@/theme/tokens/shadows"
import { zIndex } from "@/theme/tokens/z-index"

export default defineConfig({
  presets: ["@pandacss/preset-base", "@pandacss/preset-panda"],

  preflight: true,

  // Park UI's distributed source needs Solid-aware styled components.
  jsxFramework: "solid",

  include: ["./src/**/*.{js,jsx,ts,tsx}", "./pages/**/*.{js,jsx,ts,tsx}"],

  exclude: ["./src/**/*.test.{ts,tsx}"],

  theme: {
    extend: {
      animationStyles,
      recipes,
      slotRecipes,
      keyframes,
      layerStyles,
      textStyles,

      tokens: {
        colors,
        durations,
        fonts,
        zIndex,
      },

      semanticTokens: {
        colors: {
          ...semanticColors,
          accent,
          amber,
          gray: sand,
          red,
          green,
          savorum,
        },

        shadows,

        radii: {
          l1: {
            value: "{radii.xs}",
          },

          l2: {
            value: "{radii.sm}",
          },

          l3: {
            value: "{radii.md}",
          },
        },
      },
    },
  },

  // Recipe variants selected at runtime are not statically knowable, so every
  // variant of these recipes is always generated.
  staticCss: {
    recipes: {
      alert: ["*"],
      collapsible: ["*"],
      recipePage: ["*"],
      ratingGroup: ["*"],
    },
  },

  outdir: "styled-system",

  globalCss,
  conditions,
})
