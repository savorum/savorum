#!/usr/bin/env bash
# Report how far the declared SREF revision is behind upstream. This is a
# nonblocking maintenance signal, not a correctness check.
set -euo pipefail

root="${1:-../sref}"
declared="$(scripts/declared-sref-commit.sh)"
upstream="$(git -C "$root" rev-parse origin/main)"

echo "declared: $declared"
echo "upstream: $upstream (origin/main)"

if [ "$declared" = "$upstream" ]; then
    echo "the pin is current"
    exit 0
fi

if ! git -C "$root" cat-file -e "$declared^{commit}" 2>/dev/null; then
    echo "the declared revision is not in this checkout, so nothing can be compared" >&2
    exit 1
fi

if ! git -C "$root" merge-base --is-ancestor "$declared" "$upstream"; then
    echo "the declared revision is not an ancestor of upstream; the histories have diverged" >&2
    echo "review both histories before moving the pin" >&2
    exit 1
fi

ahead="$(git -C "$root" rev-list --count "$declared..$upstream")"
echo "upstream is $ahead commit(s) ahead:"
git -C "$root" log --oneline "$declared..$upstream" | sed 's/^/  /'

# What changed matters more than how much. A registry, schema or conformance
# change is a reason to look now; everything else can wait for the next pin.
echo
echo "changes in the parts Savorum is tested against:"
if git -C "$root" diff --name-only "$declared..$upstream" -- registry schema conformance spec.md | grep . | sed 's/^/  /'; then
    echo
    echo "review these before moving the pin"
else
    echo "  none"
fi
exit 1
