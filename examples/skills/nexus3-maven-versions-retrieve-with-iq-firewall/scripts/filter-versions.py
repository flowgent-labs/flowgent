#!/usr/bin/env python3
"""
Filter and deduplicate Maven dependency versions from Nexus3 search results.

Reads Nexus3 /service/rest/v1/search JSON from stdin, writes filtered top-N
version list to stdout.

Environment:
  TOPN    — max versions to return (default 3)
  CURVER  — current version to exclude (default "")

Filters:
  - Removes versions containing "SNAPSHOT" (case-insensitive)
  - Removes current version (CURVER)
  - Returns at most TOPN results
"""

import sys
import os
import json


def main():
    data = json.load(sys.stdin)
    items = data.get("items", [])

    curver = os.environ.get("CURVER", "")
    topn = int(os.environ.get("TOPN", "3"))

    versions = []
    for item in items:
        v = item.get("version", "")
        if "SNAPSHOT" in v.upper():
            continue
        if v == curver:
            continue
        versions.append(v)

    result = versions[:topn]
    print(json.dumps(result))


if __name__ == "__main__":
    main()
