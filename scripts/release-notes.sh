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
    echo "Changes since $earlier are in CHANGELOG.md."
fi
echo
echo "- \`$image:$tag\`"
echo "- \`$image:$tag-documents\`, which adds PDF and photo import"
