#!/usr/bin/env python3
"""Validate SemVer and calculate rolling tags from all published releases."""
import json
import os
import re

PATTERN = re.compile(r"^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$")


def parse(tag):
    match = PATTERN.fullmatch(tag)
    if not match:
        raise ValueError("Release-Tag muss vX.Y.Z oder vX.Y.Z-beta.1 entsprechen")
    suffix = match[4]
    if suffix and any(x.isdigit() and len(x) > 1 and x.startswith("0") for x in suffix.split(".")):
        raise ValueError("Numerische Vorabversionsanteile dürfen keine führenden Nullen enthalten")
    return tuple(map(int, match.group(1, 2, 3))), suffix


def tags_for(tag, prerelease, releases):
    version, suffix = parse(tag)
    if bool(suffix) != bool(prerelease):
        raise ValueError("GitHub-Vorabversionsstatus und Release-Tag müssen übereinstimmen")
    tags = [tag[1:]]
    if suffix:
        return tags
    stable = [version]
    for release in releases:
        if release.get("draft") or release.get("prerelease"):
            continue
        try:
            candidate, pre = parse(release["tag_name"])
        except (KeyError, ValueError):
            continue
        if not pre:
            stable.append(candidate)
    if version == max(stable):
        tags.append("latest")
    if version == max(v for v in stable if v[0] == version[0]):
        tags.append(str(version[0]))
    if version == max(v for v in stable if v[:2] == version[:2]):
        tags.append("%d.%d" % version[:2])
    return tags


if __name__ == "__main__":
    event = json.load(open(os.environ["GITHUB_EVENT_PATH"]))
    release = event["release"]
    pages = json.load(open(os.environ["RELEASES_FILE"]))
    releases = [r for page in pages for r in page]
    tags = tags_for(release["tag_name"], release["prerelease"], releases)
    with open(os.environ["GITHUB_OUTPUT"], "a") as out:
        out.write("version=" + tags[0] + "\n")
        out.write("tags=" + ",".join(tags) + "\n")
