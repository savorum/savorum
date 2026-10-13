# Publish Savorum to Community Applications

This is the maintainer runbook for publishing the Unraid template. This
directory is the source for the public `savorum/unraid-templates` repository
(branch `main`), and its `README.md` becomes that repository's README.

## Release and submit

1. **Publish the approved application source and image release.** Savorum's
   release workflow runs on semantic-version `v*` tags, passes CI and its native
   container smoke tests, then publishes `ghcr.io/savorum/savorum` and its
   `-documents` variant. Make the package public for anonymous pulls.
2. **Verify the image.** The template follows the `documents` tag, which each
   stable release moves to its document-tools image. Inspect it:

   ```sh
   docker buildx imagetools inspect ghcr.io/savorum/savorum:documents
   ```

   To check anonymous access without using saved registry credentials:

   ```sh
   registry_check_dir="$(mktemp -d)"
   docker --config "$registry_check_dir" manifest inspect ghcr.io/savorum/savorum:documents
   rmdir "$registry_check_dir"
   ```

   Confirm runtime entries for `linux/amd64` and `linux/arm64`, excluding
   attestation manifests, plus the image user, entrypoint, default command,
   health check, and `/data` defaults.
3. **Publish the template repository.** Keep it public, active, and unarchived.
   Copy `README.md`, `PUBLISHING.md`, `LICENSE`, `icon.png`, `ca_profile.xml`, and
   `templates/` to its root. Keep the root OSI-approved license and non-empty
   profile. Check every public resource listed below without signing in.
4. **Check XML and Unraid behavior.** From the template repository root:

   ```sh
   xmllint --noout ca_profile.xml templates/savorum.xml
   ```

   Complete the manual checks below with the selected release image.
5. **Validate, Scan, and submit for review.** Open
   [the submission flow](https://ca.unraid.net/submit/new), enter the public
   repository, run **Validate**, then **Scan**, and resolve findings. Submit for
   review after checks pass. Repeat Validate and Scan after meaningful XML
   edits. Acceptance requires review.

Use the current [submission help](https://ca.unraid.net/submit/help),
[repository format](https://ca.unraid.net/submit/help/repository-xml),
[profile format](https://ca.unraid.net/submit/help/repository-info-xml), and
[field reference](https://ca.unraid.net/submit/help/xml-field-reference).

## Public prerequisites

Check each of these without signing in, along with the raw URLs of every file
in the template repository:

- The released package `ghcr.io/savorum/savorum:documents` and its
  [package page](https://github.com/savorum/savorum/pkgs/container/savorum).
- The screenshots at `https://savorum.app/images/library-compact.webp` and
  `https://savorum.app/images/cooking.webp`.
- The documentation at `https://docs.savorum.app/`, which the README links to.

## Manual Unraid checks

- Install with a dedicated `65532:65532` appdata folder and a free host port.
  Check health and logs, then open WebUI from another LAN device. Confirm the
  configured address, image user, and private data permissions.
- Add one small recipe and photograph. Restart, then recreate only the
  container with the same appdata folder. Confirm both items persist.
- Stop cleanly, back up to separate storage, and restore into a separate empty
  test appdata folder. Verify the backup and open the recipe and photograph.
  Preserve webhook keys according to the recovery guide.
- Back up before applying an update from the Docker tab. Check migration,
  health, recipe access, and photographs. Recover using the backup and its
  compatible image; do not assume a database downgrade is supported.
- If using a proxy, test HTTPS, Savorum address, trusted proxy settings, sign-in,
  and browser sessions. Check Unraid autostart and shutdown if enabled.
