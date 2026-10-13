# Security policy

Report a vulnerability privately through
[GitHub private vulnerability reporting](https://github.com/savorum/savorum/security/advisories/new).
Do not include exploit details in a public issue.

Include the affected version or commit, the deployment configuration, the steps
that reproduce the issue, and the effect.

## What happens next

1. The project acknowledges a report within 7 days and assesses affected
   versions.
2. The disclosure timeline is 90 days from the report, or earlier when a fix
   is released.
3. A confirmed vulnerability is published as a GitHub security advisory once a
   fix is available, naming the affected and fixed versions.

## Supported versions

The latest release and the default branch receive security fixes. Other
versions do not.

## Scope

The server, its HTTP API, the container image, and the command-line tool are in
scope. Recipe content, uploaded files, imported documents, and web pages fetched
for import are untrusted. A report is in scope when crafted input from one of
them:

- runs code, reads or writes files, or makes network requests the deployment
  did not allow
- crosses an account or library boundary, or bypasses authentication or
  authorization
- uses memory, time, or disk beyond the configured limits
- exposes credentials, tokens, or other secrets

The trust boundaries and the controls each release review checks are in the
[security threat model](docs/reference/security-threat-model.md).

The security of the SREF specification and its reference libraries is covered
by those repositories' policies.
