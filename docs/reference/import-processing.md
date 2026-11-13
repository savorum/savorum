# Import processing

Read this before you change how Savorum fetches or parses import input. Import
input is untrusted, and the security threat model relies on the rules below.
For how cooks import recipes, see
[Import recipes](https://docs.savorum.app/guide/import-recipes/).

## Outbound requests

- URL imports accept only HTTP and HTTPS URLs without embedded credentials.
- Webhook deliveries follow the same address checks and accept only HTTPS.
  They reach private networks only when listed in
  `--webhook-trusted-networks`.
- Every DNS result and redirect target is checked immediately before dialing.
  Loopback, private, link-local, reserved, and other non-public addresses are
  blocked.
- Page and image requests use a credential-free client. Authentication, proxy
  and cookie headers are stripped again on redirects, and redirects carry no
  `Referer`.
- Page requests prefer HTTP/2 and send `Accept`, `Accept-Encoding: gzip`, and
  `Accept-Language` in the order browsers do. Some sites refuse other orders, or
  a request that names no language. A gzipped page is decoded within the page
  byte limit.
- Every inbound and outbound stage has a finite time limit. Keep it that way
  when you add one.

## Source addresses

Saved provenance never keeps URL userinfo, query strings, or fragments. A
meaningful query parameter can't be told apart from a bearer credential, so all
of them go. Sites whose recipe identity depends on a query need a query-free
canonical or JSON-LD URL; otherwise Savorum records the query-free path and
uses it for duplicate matching.

## Page text

HTML character decoding follows the HTTP charset declaration or the page's
encoding metadata. Decoded HTML shares the page byte limit. The original page
bytes remain unchanged.

Recipe language uses the first valid declaration from the recipe, its metadata
graph, or the HTML page. Invalid declarations produce a review warning. A
stated English, or no stated language, is checked against the ingredient lines:
when another language's units name at least three measures and twice as many
as English does, the recipe takes that language.

JSON-LD decoding escapes literal line breaks and tabs inside strings while
preserving their values. A capitalized property name, such as `"Name"`, is read
as its Schema.org form when that form is absent. A page without JSON-LD Recipe
data is read from Schema.org microdata, then from an
[h-recipe](https://microformats.org/wiki/h-recipe). A microdata ingredient or
instruction element that holds several lines gives one entry per line, and a
wholly bold ingredient line titles the lines after it.

Article extraction reads HTML landmarks, [h-entry and hentry](https://microformats.org/wiki/h-entry)
entries, and [Schema.org article bodies](https://schema.org/articleBody). Nested
entries remain separate. Explicit section labels and entry boundaries govern
ingredient and instruction extraction:

- A label may say what its section is for, such as "Ingredients for the dough"
  or "なす田楽の材料". A further label of the same kind within a section starts
  a titled group, as does a short line between two ingredient lists.
- Ingredient lines may give the amount first or last, in digits or words, and
  may start with a bullet character. The ingredient parser decides whether a
  line states an amount.
- Ingredient lines written in paragraphs end at a paragraph of sentences.
- Steps may be numbered "1.", "1)", "1-", "1.-", or "①". A paragraph holding
  only a step number, a photo, or a duration is skipped.
- An untitled method is a list or paragraphs, not both. Steps under their own
  sub-headings may be either.
- A heading after the ingredients that leads straight into numbered steps, or
  into a numbered step heading such as "Step 1" or "①", is the method heading,
  whatever its wording.
- A short numbered line followed by a paragraph that is not numbered titles
  that paragraph's steps. A short line that is not numbered does the same at
  the start of the method or after another titled part.
- Numbered steps end at the first unnumbered paragraph that no numbered step
  follows.
- A label followed by a duration, such as "Preparation" then "15 minutes" or
  "Preparation 15 minutes", is a timing, not a section.
- On a page that labels no method, paragraphs of steps after an ingredient list
  are its method.
- Ingredients set out in a table of at most three columns, or in a definition
  list, are read one row or term per line; a single-cell row titles the rows
  after it. Any other table that holds a section leaves the reading uncertain.
- A line that only states the servings, such as "6 people", is not an
  ingredient. A group of radio buttons, such as a servings switch, is not
  recipe content.
- An equipment, notes, or tips label, a list of links, a table, or a paragraph
  that is not a step ends a section. A link to another recipe between steps is
  skipped.

A page that marks no landmark, entry, or article body is read within the
smallest element that holds an ingredients label and the instructions label
next to it. Only a complete reading is imported. When the page repeats the
recipe in several languages, the one in the page's language is imported.

Recipe records that share an identifier, or a name without an address,
describe one recipe; the one with the most ingredients and steps is read. When
a page's Recipe data names the recipe without its ingredients or method, and
the article names the same recipe, the article supplies them. Names match when
one contains the other, or when they open with the same words, two or more
and at least 60% of the shorter name. Names written without spaces compare
characters, four or more. When the Recipe data has ingredients but no method, a single
labelled method on the page supplies the steps even if the article is not a
complete recipe. Recipe data may list its ingredients under the older
`ingredients` property.

When an element exceeds the HTML attribute limit, a bounded token scan can
read JSON-LD without constructing the page tree. Page, token, JSON, cancellation,
and extraction work limits apply. The preview warns that page details may be
missing.

## Unlabeled measures

A cup or spoon is a different size in different countries. A page whose
language names no region, such as `lang="en"`, leaves those measures
unidentified, and review asks which convention they follow.

A web import answers that question from the page's address when the recipe is
in English or states no language, and the last label of its host has a usual
convention:

| Host ends in                      | Convention      |
| --------------------------------- | --------------- |
| `.com`, `.us`, `.gov`, `.edu`     | US customary    |
| `.au`                             | Australian      |
| `.uk`, `.ie`, `.nz`, `.ca`, `.za` | Metric culinary |

The answer is a resolution decision, not source evidence. The import job
records it with the basis `source-address`, and review says which convention
was used and offers to change it. A measure the convention does not cover, such
as a pint under metric culinary, is still asked about.

Every import also answers a measure whose written form has one meaning in the
registry and no regional convention, such as `g` on a page whose language tag
the registry does not list. That decision has the basis `sole-reading` and is
not asked about.

## Limits people hit

| Boundary                       |                                          Limit |
| ------------------------------ | ---------------------------------------------: |
| Imported recipe page           |                                          8 MiB |
| Single HTML token              |                                        256 KiB |
| Imported remote image          |                                         16 MiB |
| PDF or image file              | 32 MiB, 8 PDF pages, 20 seconds per extraction |
| Document extractions at a time |     1 per process; overlapping work is refused |
| Import preview sessions        |              96 MiB each, 256 MiB per instance |
| Durable asset capacity         |                       10 GiB or 50,000 objects |
| Unreferenced asset capacity    |                                          2 GiB |
| Reserved filesystem headroom   |                                        512 MiB |

Oversized raw-text bodies are omitted from the HTML tree. Caption extraction
scans script data for matching post objects up to 256 KiB without decoding the
surrounding application state. The JSON nesting and extraction work limits apply.
Profile pages are read for `external_url` string values in the page bytes,
because their inline resources exceed the HTML attribute budget. Sitemaps are
read up to 50,000 entries.

The other budgets are constants next to the code that enforces them.

## Social post requests

A social post import can request more than the post itself:

- up to three links the post or its author's comments publish
- the post's canonical page, when the requested page omits its comments
- when the post yields only a draft, the creator's profile page, then for up to
  two creator websites `robots.txt` and up to six sitemaps, and the one page
  whose address best matches the post

Comments count only when the post's author wrote them. A creator website is a
homepage the author linked, or a profile link whose host contains the account
name.

## Document tools

PDF and image imports run Poppler and Tesseract as child processes with CPU,
file size, and (on Linux) address space limits. These per-process limits are
not an aggregate memory bound. Untrusted or cloud hosting needs a host or
container memory limit.

## Uploaded assets

Never delete content-addressed files directly. Run `savorum gc-assets`, which
rechecks database references in a transaction so current and retained
revisions stay protected.
