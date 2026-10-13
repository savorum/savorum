import { defineSlotRecipe } from "@pandacss/dev"

export const recipePage = defineSlotRecipe({
  className: "recipe-page",
  slots: [
    "root",
    "toolbar",
    "toolbarActions",
    "toolbarGroup",
    "toolbarRule",
    "masthead",
    "headline",
    "title",
    "description",
    "meta",
    "image",
    "controls",
    "controlGroup",
    "controlLabel",
    "controlRule",
    "body",
    "ingredients",
    "instructions",
    "columnTitle",
    "section",
    "ingredientSection",
    "sectionTitle",
    "ingredientList",
    "ingredient",
    "amount",
    "steps",
    "step",
    "stepNumber",
  ],
  base: {
    // A column, so the order of its parts can differ from the order they are
    // written in. On a narrow screen the recipe has to come before the things
    // a reader might do to it; on a wide one the row of controls fits above
    // the title without pushing it anywhere.
    root: {
      display: "flex",
      flexDirection: "column",
      marginInline: "auto",
      maxWidth: "84rem",
      paddingBlock: { base: "6", md: "8" },
      paddingInline: { base: "5", md: "8", lg: "10" },
    },
    // One row above the recipe where there is room. Narrow, the box dissolves:
    // the way back stays at the top and the actions follow the recipe. Groups
    // are separated by a rule so the order survives wrapping.
    toolbar: {
      alignItems: "center",
      display: { base: "contents", md: "flex" },
      flexWrap: "wrap",
      gap: "3",
      justifyContent: "space-between",
      marginBottom: "5",
    },
    toolbarActions: {
      alignItems: "center",
      display: "flex",
      flexWrap: "wrap",
      gap: "2",
      justifyContent: { base: "flex-start", md: "flex-end" },
      marginBottom: { base: "6", md: "0" },
      order: 3,
    },
    toolbarGroup: {
      alignItems: "center",
      display: "flex",
      flexWrap: "wrap",
      gap: "2",
    },
    toolbarRule: {
      alignSelf: "stretch",
      background: "border.default",
      // Hidden where the row has wrapped and the rule would sit between two
      // lines pointing at nothing.
      display: { base: "none", sm: "block" },
      marginInline: "1",
      minHeight: "1.5rem",
      width: "1px",
    },
    masthead: {
      order: 2,
      alignItems: "start",
      display: "grid",
      gap: { base: "5", md: "10" },
      paddingBottom: { base: "6", md: "8" },
    },
    headline: {
      minWidth: "0",
    },
    title: {
      color: "fg.default",
      fontFamily: "editorial",
      fontSize: { base: "3xl", sm: "4xl", md: "5xl" },
      fontWeight: "normal",
      letterSpacing: "-0.025em",
      lineHeight: "1.06",
      margin: "0",
      textWrap: "balance",
    },
    description: {
      color: "fg.muted",
      fontSize: "md",
      lineHeight: "1.55",
      marginTop: "3",
      maxWidth: "48rem",
    },
    meta: {
      alignItems: "center",
      color: "fg.muted",
      display: "flex",
      flexWrap: "wrap",
      fontSize: "sm",
      rowGap: "1",
      columnGap: "3",
      marginTop: "4",
    },
    image: {
      aspectRatio: "4 / 3",
      borderRadius: "l2",
      display: "block",
      objectFit: "cover",
      width: "100%",
    },
    // How much to make and which measures to read it in are both display
    // choices, and neither edits the recipe. They answer different questions,
    // so they are two labelled groups rather than one run of controls.
    controls: {
      alignItems: "center",
      display: "flex",
      flexWrap: "wrap",
      rowGap: "2",
      columnGap: "3",
      marginTop: "3",
    },
    controlGroup: {
      alignItems: "center",
      display: "flex",
      flexWrap: "wrap",
      rowGap: "2",
      columnGap: "2",
    },
    controlLabel: {
      color: "fg.muted",
      fontSize: "xs",
      fontWeight: "semibold",
      letterSpacing: "0.04em",
    },
    controlRule: {
      alignSelf: "stretch",
      background: "border.default",
      display: { base: "none", md: "block" },
      marginInline: "1",
      minHeight: "1.75rem",
      width: "1px",
    },
    // Ingredients and method are independent reading columns. Method prose
    // never slips underneath the ingredient list: that makes step width change
    // midway through a recipe and is especially disorienting on long breads.
    body: {
      order: 4,
      borderTopWidth: "1px",
      borderColor: "border.subtle",
      display: "grid",
      gap: { base: "0", lg: "12" },
      gridTemplateColumns: {
        base: "1fr",
        lg: "minmax(18rem, 23rem) minmax(0, 1fr)",
      },
      paddingTop: { base: "6", md: "8" },
    },
    // One amount column for every group, so names line up down the whole
    // list rather than jumping between groups whose amounts differ in width.
    ingredients: {
      alignContent: "start",
      display: "grid",
      gridTemplateColumns: "fit-content(9rem) minmax(0, 1fr)",
      marginBottom: { base: "8", lg: "6" },
      minWidth: "0",
    },
    instructions: {
      minWidth: "0",
      maxWidth: "70ch",
    },
    columnTitle: {
      gridColumn: "1 / -1",
      color: "fg.muted",
      fontSize: "xs",
      fontWeight: "semibold",
      letterSpacing: "0.04em",
      margin: "0",
      marginBottom: "4",
      textTransform: "none",
    },
    section: {
      breakInside: "avoid",
      gridColumn: "1 / -1",
      marginBottom: "7",
    },
    ingredientSection: {
      breakInside: "avoid",
      display: "grid",
      gridColumn: "1 / -1",
      gridTemplateColumns: "subgrid",
      marginBottom: "7",
    },
    sectionTitle: {
      gridColumn: "1 / -1",
      color: "fg.default",
      fontFamily: "editorial",
      fontSize: "xl",
      fontWeight: "normal",
      lineHeight: "1.25",
      margin: "0",
      marginBottom: "3",
    },
    // The amounts share one column across the whole list, sized to the
    // longest of them, so a converted or scaled compound amount such as
    // "2 tbsp + 1 tsp" is not broken across lines between its own two terms.
    // The cap keeps one unusually long amount from crowding out the names.
    ingredientList: {
      display: "grid",
      gridColumn: "1 / -1",
      gridTemplateColumns: "subgrid",
      listStyle: "none",
      margin: "0",
      padding: "0",
    },
    ingredient: {
      display: "grid",
      fontSize: "md",
      gridColumn: "1 / -1",
      gridTemplateColumns: "subgrid",
      overflowWrap: "anywhere",
      lineHeight: "1.5",
      paddingBlock: "1.5",
    },
    amount: {
      color: "fg.default",
      fontVariantNumeric: "tabular-nums",
      fontWeight: "semibold",
      // Amounts are formatted by the interface. Taking their direction from
      // their own text keeps "10 g" in order inside text of either direction.
      unicodeBidi: "plaintext",
      // The floor keeps short amounts in one column; both it and the
      // separation fall away for a section with no amounts.
      marginInlineEnd: "3",
      minWidth: "3.25rem",
      "&[data-empty]": { marginInlineEnd: "0", minWidth: "0" },
    },
    steps: {
      listStyle: "none",
      margin: "0",
      padding: "0",
    },
    step: {
      color: "fg.default",
      fontFamily: "editorial",
      fontSize: { base: "md", md: "lg" },
      lineHeight: "1.6",
      marginBottom: "5",
      display: "grid",
      gridTemplateColumns: "2.25rem minmax(0, 1fr)",
      alignItems: "baseline",
      "& > :not(:first-child)": { gridColumn: "2" },
      // A step is a link target, and one scrolled flush to the top of the
      // viewport reads as though the method starts there. The margin leaves
      // the step it followed just visible above it.
      scrollMarginBlockStart: "6",
    },
    stepNumber: {
      color: "accent.default",
      display: "inline-block",
      fontFamily: "body",
      fontSize: "sm",
      fontVariantNumeric: "tabular-nums",
      fontWeight: "semibold",
    },
  },
  defaultVariants: { illustrated: false },
  variants: {
    // A photograph earns a column of its own; without one the text uses the
    // whole width rather than leaving a reserved gap.
    illustrated: {
      true: {
        masthead: {
          gridTemplateColumns: {
            base: "1fr",
            md: "minmax(0, 1fr) minmax(0, 20rem)",
          },
        },
      },
      false: { masthead: { gridTemplateColumns: "1fr" } },
    },
  },
})
