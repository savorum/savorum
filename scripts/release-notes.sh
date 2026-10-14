#!/usr/bin/env bash
# Prints the GitHub release text for one tag, as SREF's release notes do.
#
#     release-notes.sh v1.2.3
set -euo pipefail

tag="${1:?usage: release-notes.sh TAG}"
image=ghcr.io/savorum/savorum
earlier="$(git tag --list 'v*' --sort=-v:refname --merged "$tag^{commit}" | grep -vx "$tag" | head -n 1 || true)"

if [[ -z "$earlier" ]]; then
    echo "First release of Savorum Core, the self-hosted recipe manager."
else
    if ! notes="$(awk -v heading="## ${tag#v}" '
        $0 == heading { found = 1; next }
        found && /^## / { exit }
        found && /^Released / { next }
        found && (NF || started) { started = 1; print }
        END { if (!found) exit 1 }
    ' CHANGELOG.md)" || [[ -z "$notes" ]]; then
        echo "missing changelog entry for $tag" >&2
        exit 1
    fi
    echo "Savorum Core, the self-hosted recipe manager."
    echo
    printf '%s\n' "$notes"
    echo
    echo "Container images:"
fi
echo
echo "- \`$image:$tag\`"
echo "- \`$image:$tag-documents\`, which adds PDF and photo import"
