#!/usr/bin/env python3
"""Scan every byte of tracked gzip members for the public-tree private patterns."""

import gzip
import re
import subprocess
import sys
from pathlib import Path


def main() -> int:
    pattern = re.compile(sys.argv[1], re.IGNORECASE)
    result = subprocess.run(
        ["git", "ls-files", "-z", "--", *sys.argv[2:]],
        capture_output=True,
        check=True,
    )
    found = False
    for name in result.stdout.split(b"\0"):
        if not name:
            continue
        path = Path(name.decode())
        if path.suffix != ".gz":
            continue
        with gzip.open(path, "rb") as stream:
            for number, line in enumerate(stream, 1):
                matches = [
                    match.group(0)
                    for match in pattern.finditer(
                        line.decode("utf-8", errors="replace")
                    )
                ]
                if matches:
                    # Report the matching markers; do not dump binary profile data.
                    print(f"{path}:{number}: private markers: {', '.join(matches)}")
                    found = True
    return int(found)


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, EOFError, UnicodeError, subprocess.CalledProcessError) as error:
        print(f"check-public: archive scan unavailable: {error}", file=sys.stderr)
        sys.exit(2)
