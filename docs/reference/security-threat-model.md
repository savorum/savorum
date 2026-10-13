# Security threat model

The release reviewer uses this page for the security review of each release
candidate. It sets out Savorum's trust boundaries, the assets at risk, and the
controls to verify. Operator instructions are on the docs site:
[configure](https://docs.savorum.app/admin/configure/),
[remote access](https://docs.savorum.app/admin/remote-access/),
[accounts](https://docs.savorum.app/admin/accounts/), and
[backup and restore](https://docs.savorum.app/admin/backup-restore/).

## What a release review covers

The review uses the exact candidate revision, its documented deployment modes,
and its `GET /api/v1/capabilities` response. A boundary is in scope when
its capability is built, enabled, and advertised in that candidate.
Configuration-dependent capabilities are reviewed in every supported
configuration that enables them. A compiled feature isn't automatically
supported: check each import path registered in
[`internal/importer`](../../internal/importer/importer.go), each sign-in
method, and each integration against the candidate's configuration and
published support. Record the revision, configuration, capability response,
evidence, and disposition outside this page.

## Deployment modes and authority

| Mode | Exposure and trusted actors | Authentication and isolation | Operator responsibility |
| --- | --- | --- | --- |
| Local single user | Default loopback HTTP; local processes and the host account can connect. | Authentication is off by default. Requests act as the persisted local user; separate browser users have no security boundary. | Keep the listener on loopback, protect the host account and data directory, and secure backups. |
| LAN | A configured LAN listener admits devices on that network. Other LAN users are untrusted. | Enable Savorum authentication for separate people, or treat the entire permitted network as one trusted user in local mode. Memberships separate libraries only with authentication. | Restrict network reachability, set the exact public URL, and use transport protection appropriate to the LAN. |
| Reverse proxy | The backend stays on loopback or a private container network; clients reach a TLS proxy. | Savorum authentication or an access-controlled proxy gates remote use. Forwarded metadata is trusted only from configured immediate peers. Proxy identity is a separate provider configuration. | Terminate TLS, strip untrusted forwarding and identity headers, preserve private asset cache headers, and restrict direct backend access. |
| Internet-facing authenticated | Remote clients are untrusted even when signed in. | Savorum sessions or configured external providers identify users; library roles and token scopes restrict actions. | Use HTTPS and a narrow proxy trust list, protect provider secrets, test recovery, and monitor resource use. Never expose authentication-disabled local mode to the public Internet. |
| Multi-user or multi-library | Signed-in users, invited users, and integrations have different authority. | Every resource read and write is scoped to current membership and role; tokens add scope and library restrictions. An instance administrator has broader authority. | Grant the least role needed, revoke access when it ends, and protect administrator and recovery access. |

### Host-level authority

The process owner, an administrator with direct database or runtime-secret
access, and an attacker with full host control can read, replace, or remove
application data. Savorum's HTTP authorization, storage modes, and archive
checks don't defend against that level of control.

- The SQLite and asset paths need a local filesystem. The default storage
  policy gives files owner-only access. The explicit group policy extends host
  trust to members of a dedicated operating-system group.
- The print compiler cache holds machine code the process runs. It's used only
  when no other user can write it.

See [configure](https://docs.savorum.app/admin/configure/) for storage
permissions and the typesetter cache.

### Outbound requests

Every mode can fetch outbound through URL import and remote image retrieval.
Configured OIDC and SAML providers, webhook destinations, and other network
services add more paths. A LAN-only listener
doesn't make those destinations trusted.

## Data flows

```text
Untrusted browser or API client
  -> [network boundary] listener or TLS reverse proxy
  -> [HTTP boundary] authority, origin, size and rate guards
  -> [identity boundary] session, provider, or token identity
  -> [authorization boundary] library and object authorization
  -> API services -> SQLite revisions, jobs and audit records
                  -> content-addressed assets and private generated artifacts
  <- authorized HTML, JSON, images, events, or downloads

Operator with local filesystem authority
  -> [host authority boundary] CLI backup/restore/migration
  -> SQLite snapshot and asset tree
  -> [backup custody boundary] archive in operator-controlled storage
  -> offline verified restore
```

```text
Untrusted URL, file, text, or archive
  -> [external input boundary] guarded fetch or bounded upload
  -> format probe and parser -> preview and staged remote bytes
  -> [commit boundary] actor/library/revision recheck
  -> SQLite recipe revision and verified assets

Verified event outbox -> [delivery boundary] library-filtered SSE
                  or signed webhook -> guarded HTTPS destination
```

**Protected assets:** recipe and library content, immutable revisions and audit
attribution, user and library separation, session and recovery material,
provider and webhook secrets, personal tokens, source attachments, SREF
exports, backups, SQLite and asset integrity, and finite disk, memory, CPU,
worker, and outbound-request capacity.

**Untrusted on entry:** imported content, browser requests, remote servers,
archive members, provider assertions, proxy headers, and backup files. A
provider assertion becomes identity evidence only after protocol validation,
and the immediate proxy peer and its header policy must also be trusted.

## Threats and verification

Each row gives the attacker's entry and capability, the affected asset, the
control, and the review test. "Networked" means LAN, reverse-proxy, or
Internet-facing operation. "All" includes local operation. Verify every control
against the candidate.

| ID | Threat, boundary, and affected modes | Control and candidate review |
| --- | --- | --- |
| H1 | A cross-site page sends an unsafe browser request to any reachable listener, changing a recipe or account. All modes. | Reject mismatched `Origin` and cross-site Fetch Metadata before body processing; require session-bound CSRF on cookie mutations. Exercise simple and multipart requests through each supported proxy path. See [remote access](https://docs.savorum.app/admin/remote-access/). |
| H2 | Authored or imported HTML, text, filenames, or URLs execute in the application origin or escape into a download. All modes. | Keep content as data, constrain rendering with CSP and MIME headers, and authorize generated files. Probe stored, reflected, DOM, print, import-preview, and error paths with active payloads. See [`internal/httpsecurity`](../../internal/httpsecurity/security.go). |
| H3 | A caller guesses another recipe, asset, job, event, source attachment, or download ID. Multi-user operation. | Authorize the object through current user, library, role, and token scope on every read and write. Exercise cross-user, cross-library, revoked, and trashed references, including `HEAD` and cached images. See [accounts](https://docs.savorum.app/admin/accounts/) and [`internal/imaging`](../../internal/imaging/). |
| H4 | A stale client or background job overwrites a newer revision or continues after authority changes. All modes for revision integrity; multi-user for authority. | Require expected revisions, recheck actor and library at job commit, and bind staged previews to their reviewed state. Race mutation, import reconciliation, cancellation, membership revocation, and job resume. See [resource lifecycle](resource-lifecycle.md). |
| H5 | A session, recovery code, passkey ceremony, or token is stolen, fixed, replayed, or remains usable after revocation. Authenticated modes. | Verify random secret handling, cookie flags, origin and challenge checks, expiry, recent authentication, one-time recovery, and immediate revocation. Test sign-out, password reset, disablement, token revocation, and recovery replay. See [`internal/identity`](../../internal/identity/) and [accounts](https://docs.savorum.app/admin/accounts/). |
| H6 | A forged or misbound external identity gains an account or library role. Configurations with OIDC, LDAP, SAML, or proxy identity. | Verify provider-specific issuer/subject, state, browser binding, signature or fresh directory bind, group mapping, and unlink behavior. Native sign-in also requires an exact return target, PKCE S256, and a one-minute single-use code. Native linking also requires a single-use ticket bound to the session and challenge, and confirmation with the verifier from the same session. Test proxy-header injection from an untrusted peer and provider change with existing links. See [single sign-on](https://docs.savorum.app/admin/single-sign-on/). |
| H7 | A hostile `Host`, forwarding header, or public URL causes origin confusion, cookie misuse, or callback misdirection. Networked modes. | Check direct and effective authority against the deployment, trust only configured immediate proxy peers, and bind provider origins to the configured URL. Test malformed and conflicting headers at both proxy and backend. See [remote access](https://docs.savorum.app/admin/remote-access/). |
| I1 | A URL, redirect, DNS change, page image, or provider-supplied location reaches an internal service or leaks a credential. All modes with outbound import; configured network integrations. | Revalidate each resolved connection and redirect, reject private/reserved destinations unless explicitly allowed for webhooks, strip credentials and sensitive provenance, and bound time and bytes. Exercise DNS rebinding, redirects, URL userinfo, and private targets for every outbound path. See [`internal/httpsecurity`](../../internal/httpsecurity/outbound.go) and [outbound requests](import-processing.md#outbound-requests). |
| I2 | A ZIP, SREF package/bundle, native export, JSON, HTML, XML, PDF, or image consumes unbounded work, escapes a destination path, or supplies duplicate members. All modes with the format enabled. | Enforce input, expanded-byte, entry, depth, parser, process, pixel, and concurrency limits across the whole operation; reject unsafe or ambiguous archive paths. Fuzz malformed formats and test aggregate nested budgets, cancellation, and temporary cleanup. See [import processing](import-processing.md). |
| I3 | Previewed remote bytes or duplicate decisions differ from the committed content, or import jobs outlive the initiating user's authority. All modes; multi-user adds authorization risk. | Retain reviewed bytes, bind preview and commit to actor, library, target and revision, and recheck authority before each batch. Race source changes, target revisions, cancellation, and membership removal. See [import jobs](resource-lifecycle.md#import-jobs). |
| I4 | Uploaded or imported media is served as active content, or an expensive `GET`/`HEAD` exhausts CPU, memory, or asset capacity. All modes. | Validate media and dimensions, serve through authorized references, use safe content headers and bounded derivatives, and enforce admission and capacity limits. Probe polyglots, malformed images, repeated `HEAD`, and variant requests. See [`internal/imaging`](../../internal/imaging/). |
| S1 | Database, migration, disk, lock, or shutdown failure corrupts acknowledged revisions or leaves an unsafe partially started instance. All modes. | Enforce SQLite migration ledger and future-version refusal, WAL/FULL policy, bounded busy handling, disk reserves, and orderly shutdown. Fault-inject migration, contention, full disks, and restart. See [migrations](database-migrations.md) and [`internal/storage/sqlite`](../../internal/storage/sqlite/). |
| S2 | Trash, purge, or asset collection discards retained history or races a new reference. All modes. | Require explicit revisioned purge, retain historical references, and recheck asset reachability in the serialized deletion transaction. Race attachment against garbage collection and verify backups of historical assets. See [resource lifecycle](resource-lifecycle.md#asset-reachability). |
| S3 | A malformed, newer, or inconsistent backup replaces a live instance, or a crash mixes database and asset generations. Operator restore path in all modes. | Verify archive layout, sizes, hashes, SQLite integrity, foreign keys and compatibility in private staging; take the instance lock and recover a journaled restore atomically. Test interruption at each replacement checkpoint and reject unsupported versions. See [backup and restore](https://docs.savorum.app/admin/backup-restore/). |
| S4 | An attacker replaces a complete backup and its manifest with a self-consistent malicious archive. Operator restore path in all modes. | The archive has no detached authenticator. The operator trusts backup storage and origin; hashes detect corruption and inconsistent modification only. Protect backup access and provenance outside Savorum, then inspect and restore a known backup in an isolated drill. A host or backup-store compromise can defeat archive verification. See [backup trust](#backup-trust-contract). |
| G1 | A token, event cursor, webhook, or temporary download discloses another library's data or remains usable after revocation. Configurations with integrations. | Apply current library membership and token scope on each resource; authorize job artifacts and filter event replay. Test cross-library, expired, revoked, and replayed artifacts. See [api.savorum.app](https://api.savorum.app/) and [accounts](https://docs.savorum.app/admin/accounts/). |
| G2 | A webhook sends an event or signing secret to an attacker-controlled internal destination, or a receiver accepts a replay. Configurations with webhooks. | Require guarded HTTPS destinations, sign the timestamp and exact body, and bound retries and queues. Test redirects and address changes; verify receiver-side timestamp and delivery-ID replay rejection. See [api.savorum.app](https://api.savorum.app/). |

### Residual risk

A trusted proxy, identity provider, webhook receiver, or backup store can
break its part of the boundary, and Savorum can't prove their configuration or
custody.

Audit records and diagnostics must omit secrets and unbounded untrusted text.
The review checks real error and failure paths.

## Backup trust contract

Backup storage and archive origin are trusted. The manifest holds file sizes
and SHA-256 hashes inside the same archive as the data. `verify` detects
accidental corruption and inconsistent changes. It doesn't authenticate a whole
archive against someone who can replace both data and manifest, and Savorum
creates and verifies no detached signature or MAC. A verified archive isn't
proof of who created it.

The operator restricts backup access, keeps a trustworthy copy outside the
instance's failure boundary, and chooses the archive to restore.
[Backup and restore](https://docs.savorum.app/admin/backup-restore/) describes
verification and recovery under this contract.

## Candidate review surfaces

Mark each threat `applicable` or `not applicable` from the candidate's
capability list, then record evidence for every applicable ID. A feature
disabled in one supported mode can still apply in another.

- Browser, API, sessions, library and user roles: H1 to H5, H7.
- Each enabled authentication provider: H5 to H7.
- Every advertised import format, remote fetch, export and media path: I1 to
  I4, H2 to H4.
- SQLite, assets, migrations, backup and restore: S1 to S4.
- Events, webhooks, tokens, jobs and temporary artifacts: G1, G2, H3 to H5, I1.

## When to review again

Review the model whenever an authentication provider, import path, remote
fetch, authorization scope, integration, token, backup/restore behavior,
executable extension, or supported deployment mode changes. Each candidate
review also checks earlier security fixes against their regression tests.

A hosted or centrally operated service, another authentication provider,
plugins or executable extensions, and any new import or integration path each
need their own trust-boundary analysis before a candidate supports them.
