#!/usr/bin/env bash
# Report or update the declared SREF revision: print what Savorum is tested
# against and what the sibling checkout has, and with --write move the
# declaration to the checkout's HEAD.
#
#     scripts/sref-pin.sh ../sref
#     scripts/sref-pin.sh ../sref --write
set -euo pipefail

root="${1:-../sref}"
mode="${2:-}"
declaration="integration/sref.json"

declared="$(python3 -c "import json;print(json.load(open('$declaration'))['sref']['commit'])")"

if [ ! -d "$root/.git" ]; then
    echo "declared: $declared"
    echo "no SREF checkout at $root, so there is nothing to compare it with" >&2
    exit 2
fi

head="$(git -C "$root" rev-parse HEAD)"
echo "declared: $declared"
echo "checkout: $head ($root)"

if [ "$declared" = "$head" ]; then
    echo "the pin is the checkout's HEAD"
    exit 0
fi

if git -C "$root" merge-base --is-ancestor "$declared" "$head" 2>/dev/null; then
    ahead="$(git -C "$root" rev-list --count "$declared..$head")"
    echo "the checkout is $ahead commit(s) ahead of the pin:"
    git -C "$root" log --oneline "$declared..$head" | sed 's/^/  /'
else
    echo "the checkout is not a descendant of the pin; they have diverged"
fi

if [ "$mode" != "--write" ]; then
    echo
    echo "run with --write to move the pin, then run the full matrix before merging"
    exit 1
fi

python3 - "$declaration" "$head" <<'PY'
import json
import pathlib
import sys

path, commit = pathlib.Path(sys.argv[1]), sys.argv[2]
declaration = json.loads(path.read_text(encoding="utf-8"))
declaration["sref"]["commit"] = commit
path.write_text(json.dumps(declaration, indent=2) + "\n", encoding="utf-8")
PY
echo "moved the pin to $head"
