# Managed hosting

This reference is for engineers who run a hosting service on Savorum: they
create instances for customers, map hosting accounts to local users, sign
session grants, collect backups and recover volumes. You don't need it to run
Savorum for yourself or your household. A customer leaving a hosting service
follows [Back up and restore](https://docs.savorum.app/admin/backup-restore/).

## Model

The **hosting service** holds customer accounts and signs short-lived grants.
Each instance is a **tenant** with a tenant ID. The **operator channel** is a
private, authenticated way to run `savorum` commands on an instance. Recipe
authorization and sessions stay local: Savorum never calls the hosting service
on a recipe request or when it validates an existing session.

Hosted startup requires account authentication and the recorded public origin.
The managed provider must match the recorded provider, issuer and tenant
audience. Automatic provisioning, a default library and provider-controlled
local recovery are not allowed on a hosted instance.

## Prepare an instance

Keep the public listener closed while you prepare a new database. Bootstrap
runs only through the operator channel, never over HTTP.

1. Write a bootstrap request:

   ```json
   {
     "operation_id": "bootstrap-example",
     "tenant_id": "tenant-example",
     "public_url": "https://recipes.example.com",
     "provider_id": "hosting",
     "issuer": "https://accounts.example.com",
     "owner": {"subject": "account-example", "display_name": "Alex"},
     "library_name": "Recipes"
   }
   ```

2. Run `savorum hosting bootstrap --database PATH --request FILE`. In one
   transaction it records the instance identity and public origin, maps the
   first local administrator to the external subject, and names the first
   library. It creates no password. The JSON receipt has `operation_id`,
   `tenant_id`, `instance_id`, `owner_user_id`, `library_id` and `outcome`.
3. Run `savorum hosting show --database PATH` to print the recorded request
   and mapping.
4. Install the provider configuration and the public verification keys:

   ```json
   {
     "providers": [{
       "id": "hosting",
       "kind": "managed",
       "display_name": "Hosting account",
       "managed": {
         "issuer": "https://accounts.example.com",
         "audience": "tenant-example",
         "authorize_url": "https://accounts.example.com/authorize",
         "token_url": "https://accounts.example.com/redeem",
         "keys_file": "/run/config/hosting-public.jwks"
       }
     }]
   }
   ```

5. Start `serve` with authentication enabled.

Repeating the same request returns `unchanged`. A different request, or an
instance already in use, changes nothing and exits 3.

A container can instead pass `--hosting-bootstrap-file PATH` to `serve`. It
applies the same request before the listener opens and is safe on every
start. A mismatch stops startup.

Keys are Ed25519 public JWKs with unique `kid` values in a JWKS. Savorum loads
them at startup. To rotate, publish the new key alongside the old one, restart,
then remove the old one and restart again.

## Operator socket

Start `serve` with
`--maintenance-socket /data/.savorum-control/maintenance.sock`. The directory
must have mode `0700`; the socket gets mode `0600`. It works with or without
`--idle-timeout`. Reach it only through your authenticated operator
execution. The public listener has no operator endpoints.

`hosting member`, `check-owner`, `revoke-member` and `erase-user` run through
this socket so the live instance applies its own storage admission. They take
`--socket` and refuse `--database`. If the socket is down or the instance
refuses the write, the command fails; it never opens a second writer.
`check-owner`, `revoke-member` and `erase-user` exist under both
`savorum hosting` and `savorum admin`.

## Admit a member

A member joins through a local invitation or an automatic Cloud invitation.
Savorum never links accounts by email address.

### Local invitation

1. A library owner creates an ordinary invitation for the role.
2. Write a request with its token and protect the file:

   ```json
   {
     "operation_id": "member-example",
     "tenant_id": "tenant-example",
     "subject": "account-member",
     "display_name": "Sam",
     "invitation_token": "LOCAL_INVITATION_TOKEN"
   }
   ```

3. Run
   `savorum hosting member --socket /data/.savorum-control/maintenance.sock --request FILE`.
   If you can pass neither a file nor standard input, use `--request-base64`.
4. Store the returned `local_user_id` and `library_id` in the hosting
   service's mapping.

### Automatic Cloud invitation

Send this through the same command, never a public endpoint. `subject` is the
verified Cloud account ID.

```json
{
  "operation_id": "member-example",
  "tenant_id": "household-example",
  "subject": "account-member",
  "display_name": "Cloud member",
  "issuer": "https://cloud.example.com",
  "bootstrap_id": "bootstrap-example",
  "owner_subject": "account-owner",
  "owner_user_id": "LOCAL_OWNER_USER_ID",
  "library_id": "INITIAL_LIBRARY_ID"
}
```

The issuer, bootstrap and first library must match what was recorded. The
owner must be an active instance administrator and owner of that library.
Savorum grants at most a viewer membership and never makes the member an
administrator.

In both modes, retrying an operation returns the original receipt. A retry
never restores access that was removed since. A changed request under the same
operation ID fails.

## Remove a member

```sh
savorum admin revoke-member --socket /data/.savorum-control/maintenance.sock \
  --operation OPERATION_ID --tenant TENANT --bootstrap BOOTSTRAP_OPERATION \
  --subject SUBJECT --user USER_ID --library LIBRARY_ID
```

Savorum disables the managed identity, revokes its sessions and personal
tokens, and removes the library membership and administrator role. Recipe data
and other libraries stay. It refuses to remove the last administrator or
library owner.

`savorum admin check-owner` takes the same flags and confirms the managed user
is an active administrator and owner of the first library. Ownership is
authority over the whole instance, so run this check before you offer a
transfer of ownership and again before you accept it.

## Erase an account

```sh
savorum admin erase-user --socket /data/.savorum-control/maintenance.sock \
  --tenant TENANT --bootstrap BOOTSTRAP_OPERATION --subject SUBJECT --user USER_ID
```

Erasure revokes credentials and sessions, removes preferences and
memberships, and anonymizes the local user. A collection left with no member
is purged; shared collections and recipes stay. A departing sole collection
owner's role passes to an existing member.

**The first owner needs a successor.** Once an existing mapped member has
accepted ownership, add `--successor ACCEPTED_OWNER_SUBJECT`. The successor
must be an active local user of the same provider and becomes an
administrator in the same transaction. The last administrator can't be erased
without one. The original bootstrap file stays valid for later starts.

## Keep the instance awake

With `--idle-timeout`, an awake lease on the operator socket prevents standby.
Take one before an external backup or upgrade:

```sh
savorum admin maintenance --socket /data/.savorum-control/maintenance.sock \
  --lease UNIQUE_OPERATION_ID_AT_LEAST_24_CHARACTERS --expires RFC3339_DEADLINE
```

- Use a fresh random lease ID each time (24 to 128 letters, digits,
  underscores or hyphens).
- The deadline is absolute and at most 15 minutes ahead. Repeating the
  acquisition doesn't extend it.
- Renew with `--expected OLD_DEADLINE --expires NEW_DEADLINE`. Release with
  `--release --expected DEADLINE`.
- Start a stopped instance before you take a lease. An instance already
  entering standby refuses it.
- Leases survive restarts. The lease keeps the instance awake only; backup and
  upgrade commands still need their own instance lock.

**Stop external work before the lease expires, and whenever a renewal isn't
acknowledged.**

## Session grants

`GET /api/v1/auth/{provider}/start` starts browser sign-in.
`auth.managed_session` in the capabilities document advertises the managed
provider. Managed providers can't be linked through the ordinary
provider-link routes.

1. Savorum sends the browser to `authorize_url` with `audience`, the exact
   `redirect_uri`, a random `state`, `code_challenge` and
   `code_challenge_method=S256`.
2. Your callback returns only `code` and `state`.
3. Savorum posts JSON with `code`, `code_verifier`, `audience` and
   `redirect_uri` to `token_url`. It sends no browser credentials and doesn't
   follow redirects.
4. Your service consumes the code once, checks current account membership and
   the hosting session that started the flow, and returns
   `{"grant":"SIGNED_JWT"}`.

The grant is signed with EdDSA by a trusted `kid`, has
`typ: savorum-session-grant+jwt`, and carries these claims:

| Claim | Value |
| --- | --- |
| `iss` | The configured issuer. |
| `aud` | The recorded tenant ID. |
| `sub` | The verified hosting account subject. |
| `jti` | A unique, single-use ID. |
| `iat`, `exp` | Issue and expiry, at most five minutes apart. |
| `origin` | The tenant's public origin. |
| `code_challenge` | The S256 challenge from step 1. |
| `local_user_id`, `library_id` | The mapped local user and a library they belong to. |

A grant never creates a user or changes a role. The mapped user and the
provider must be enabled, and the user must still hold a role in the library.

### Native apps

When `auth.managed_native_session` is advertised, a native app holding a
hosting account redeems its grant at
`POST /api/v1/auth/{provider}/managed-sessions` with a JSON body of `grant`
and `code_verifier`. The `Origin` header must be the tenant origin. The grant
is consumed once and the response is the ordinary session cookie, CSRF token
and account. A lost response needs a fresh grant. The endpoint doesn't accept
hosting bearer tokens. The full request shape is in the
[API reference](https://api.savorum.app/).

## Backup collection

Start `serve` with `--backup-keys-file` (an Ed25519 JWKS) and
`--backup-issuer`. The instance must have a hosting bootstrap. Each request
carries `Authorization: Bearer TOKEN`, an EdDSA JWT with
`typ: savorum-backup+jwt`:

| Claim | Value |
| --- | --- |
| `iss` | The value of `--backup-issuer`. |
| `aud` | The recorded tenant ID. |
| `sub` | The HTTP method, one space, and the request URI with its query. |
| `jti` | A unique ID. A repeat is rejected until the token expires. |
| `iat`, `exp` | Issue and expiry, at most five minutes apart. |

1. `POST /api/v1/hosting/backups/incremental?max_bytes=BOUND` returns `202`
   with the collection `id`. `409` means a collection is already running.
2. `GET /api/v1/hosting/backups/incremental/{id}` returns `202` while
   preparing, `500` on failure, or a tar stream of the manifest, SQLite
   snapshot and referenced webhook keys.
3. `GET /api/v1/hosting/backups/incremental/{id}/assets/{sha256}` streams one
   original. Fetch only originals missing from your previous verified
   snapshot.
4. `DELETE /api/v1/hosting/backups/incremental/{id}` releases the collection.

A collection expires 30 minutes after creation. A failure while streaming
aborts the connection. Originals are pinned with hard links, so the backup
workspace and the asset directory must be on the same filesystem.

`savorum admin snapshot` produces the same tree from a stopped instance.

### Verify a backup

`savorum verify --json` reports the instance and tenant identity, row counts
and content fingerprints per table, and the webhook keys the backup needs.
`--tenant-id ID` rejects another tenant's archive. `--webhook-secrets DIR`
checks that each referenced key is present. Fingerprints describe the
snapshot after the verifying binary migrates it, so compare fingerprints only
between binaries with the same schema.

## Volume recovery

`savorum admin volume-recovery --volume PATH` restores a replacement volume
while the instance is stopped. The manifest, passed as `--file`, `--request`
(base64, or `-` for standard input) or fetched from `--source`, pins the
operation, tenant state, file sizes and SHA-256 hashes. You fence other
writers and check placement before each step.

`--action` is one of:

- `status` (the default): report progress.
- `hold`: wait offline until stopped.
- `fetch`: download one manifest file from the HTTPS URL in `--source`, such
  as a presigned object URL. Each call resumes at the journaled offset and
  returns the new one; repeat until it equals the file size.
  `--fetch-timeout` is at most 90 seconds. URLs with credentials and
  redirects are refused.
- `restore`, then `verify`: install the files and check the database, the
  pinned state, every asset's hash and the webhook keys.

Run as root on a volume owned by another user, the command switches to that
user before it touches files. Set `TMPDIR` to a directory on the recovery
volume that the service user can write.

`savorum admin disk-capacity --mount PATH` reports the filesystem size. Add
`--database PATH` to include the recorded hosting identity, and `--geometry`
(Linux, ext4 block devices only) for device and block details.
