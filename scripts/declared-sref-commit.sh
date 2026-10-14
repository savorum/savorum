#!/usr/bin/env sh
# The revision this repository declares it is built and tested against.
#
#     scripts/declared-sref-commit.sh            # the specification
#     scripts/declared-sref-commit.sh sref_reader
#
# CI, justfile recipes, and contributors all resolve pins through this.
set -eu
exec python3 - "$@" <<'PY'
import json, re, sys
key = sys.argv[1] if len(sys.argv) > 1 else "sref"
declaration = json.load(open("integration/sref.json", encoding="utf-8"))
if key not in declaration:
    raise SystemExit(f"integration/sref.json declares no {key!r}")
commit = declaration[key]["commit"]
if not isinstance(commit, str) or not re.fullmatch(r"[0-9a-f]{40}", commit):
    raise SystemExit(f"integration/sref.json requires a full commit hash for {key!r}")
print(commit)
PY
