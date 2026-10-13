import { parseRational } from "./rational"
import { cookNumber } from "./recipe-format"

/** Keep source quantities exact in storage and use the recipe reader's kitchen notation. */
export function shoppingQuantity(text: string): string {
  return text.replace(/^(\d+\/\d+)(?=\s|$)/, (amount) => {
    const value = parseRational(amount)
    return value ? cookNumber(value) : amount
  })
}
