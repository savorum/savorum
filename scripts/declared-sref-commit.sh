#!/usr/bin/env sh
# The revision this repository declares it is built and tested against.
#
#     scripts/declared-sref-commit.sh            # the specification
#     scripts/declared-sref-commit.sh sref_reader
#
# CI, justfile recipes, and contributors all resolve pins through this.
set -eu
exec python3 -c '
import json, sys
key = sys.argv[1] if len(sys.argv) > 1 else "sref"
declaration = json.load(open("integration/sref.json", encoding="utf-8"))
if key not in declaration:
    raise SystemExit(f"integration/sref.json declares no {key!r}")
print(declaration[key]["commit"])
' "$@"
