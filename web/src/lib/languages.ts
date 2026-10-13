import { languageName, nativeLanguageName } from "./direction"
import { presentationContext } from "./locale-format"

/** Recipe languages offered by name. Any other valid tag a recipe carries is kept and shown too. */
export const recipeLanguageTags = [
  "en",
  "en-AU",
  "en-CA",
  "en-GB",
  "en-US",
  "ar",
  "cs",
  "da",
  "de",
  "el",
  "es",
  "fa",
  "fi",
  "fr",
  "he",
  "hi",
  "hu",
  "id",
  "it",
  "ja",
  "ko",
  "nb",
  "nl",
  "pl",
  "pt",
  "pt-BR",
  "pt-PT",
  "ro",
  "ru",
  "sv",
  "th",
  "tr",
  "uk",
  "ur",
  "vi",
  "zh-Hans",
  "zh-Hant",
]

/** "French (Français)": the interface's name, then the language's own when they differ. */
export function recipeLanguageLabel(tag: string): string {
  const local = languageName(tag, presentationContext().locale)
  const native = nativeLanguageName(tag)
  return local.toLocaleLowerCase() === native.toLocaleLowerCase() ? native : `${local} (${native})`
}

/** Languages as picker choices, ordered by their names in the interface language. */
export function languageChoices(tags: readonly string[]): { value: string; label: string }[] {
  return tags
    .map((tag) => ({ value: tag, label: recipeLanguageLabel(tag) }))
    .sort((left, right) => left.label.localeCompare(right.label, presentationContext().locale))
}

/** Choices for a language picker, keeping a current tag the list does not name. */
export function recipeLanguageChoices(
  current: string | undefined,
): { value: string; label: string }[] {
  const tags =
    current && !recipeLanguageTags.includes(current)
      ? [...recipeLanguageTags, current]
      : recipeLanguageTags
  return languageChoices(tags)
}
