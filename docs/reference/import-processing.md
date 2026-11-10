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
- Every inbound and outbound stage has a finite time limit. Keep it that way
  when you add one.

## Source addresses

Saved provenance never keeps URL userinfo, query strings, or fragments. A
meaningful query parameter can't be told apart from a bearer credential, so all
of them go. Sites whose recipe identity depends on a query need a query-free
canonical or JSON-LD URL; otherwise Savorum records the query-free path and
uses it for duplicate matching.

## Limits people hit

| Boundary | Limit |
| --- | ---: |
| Imported recipe page | 8 MiB |
| Single HTML token | 256 KiB |
| Imported remote image | 16 MiB |
| PDF or image file | 32 MiB, 8 PDF pages, 20 seconds per extraction |
| Document extractions at a time | 1 per process; overlapping work is refused |
| Import preview sessions | 96 MiB each, 256 MiB per instance |
| Durable asset capacity | 10 GiB or 50,000 objects |
| Unreferenced asset capacity | 2 GiB |
| Reserved filesystem headroom | 512 MiB |

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
