#!/usr/bin/env python3
import argparse
import json
import os
import re
import sys
import urllib.request

SEMVER = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+$")
RELEASE_URL = "https://api.github.com/repos/openai/codex/releases/latest"
OAUTH_FILE = "internal/oauth/oauth.go"
README_FILE = "README.md"


def version_from_release(payload):
    name = str(payload.get("name") or "").strip()
    tag = str(payload.get("tag_name") or "").strip()
    if tag.startswith("rust-v"):
        tag = tag[len("rust-v") :]
    for candidate in (name, tag):
        if SEMVER.fullmatch(candidate):
            return candidate
    raise ValueError("latest release has no stable X.Y.Z version")


def fetch_latest(token):
    headers = {
        "Accept": "application/vnd.github+json",
        "User-Agent": "codex-gateway-version-check",
    }
    if token:
        headers["Authorization"] = "Bearer " + token
    request = urllib.request.Request(RELEASE_URL, headers=headers)
    with urllib.request.urlopen(request, timeout=30) as response:
        payload = json.load(response)
    return version_from_release(payload)


def current_version(text):
    match = re.search(r'Version\s+=\s+"([0-9]+\.[0-9]+\.[0-9]+)"', text)
    if not match:
        raise ValueError("Version constant not found")
    return match.group(1)


def replace_oauth(text, latest):
    updated, user_agent = re.subn(
        r'(UserAgent\s+=\s+"codex-tui/)[0-9]+\.[0-9]+\.[0-9]+',
        r"\g<1>" + latest,
        text,
        count=1,
    )
    updated, version = re.subn(
        r'(Version\s+=\s+")[0-9]+\.[0-9]+\.[0-9]+(")',
        r"\g<1>" + latest + r"\2",
        updated,
        count=1,
    )
    if user_agent != 1 or version != 1:
        raise ValueError("oauth version patterns were not found")
    if 'MinVersion = "' + latest + '"' in updated and 'MinVersion = "' + latest + '"' not in text:
        raise ValueError("refusing to change MinVersion")
    return updated


def replace_readme(text, latest):
    updated, user_agent = re.subn(
        r"(codex-tui/)[0-9]+\.[0-9]+\.[0-9]+",
        r"\g<1>" + latest,
        text,
        count=1,
    )
    updated, version = re.subn(
        r"(version: )[0-9]+\.[0-9]+\.[0-9]+",
        r"\g<1>" + latest,
        updated,
        count=1,
    )
    if user_agent != 1 or version != 1:
        raise ValueError("README version patterns were not found")
    return updated


def write_output(latest, previous, changed):
    path = os.environ.get("GITHUB_OUTPUT")
    if not path:
        return
    with open(path, "a", encoding="utf-8") as handle:
        handle.write(f"changed={'true' if changed else 'false'}\n")
        handle.write(f"version={latest}\n")
        handle.write(f"previous={previous}\n")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--version", help="stable X.Y.Z to apply instead of fetching")
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument("--root", default=".")
    args = parser.parse_args()
    root = args.root
    oauth_path = os.path.join(root, OAUTH_FILE)
    readme_path = os.path.join(root, README_FILE)
    oauth_text = open(oauth_path, encoding="utf-8").read()
    readme_text = open(readme_path, encoding="utf-8").read()
    current = current_version(oauth_text)
    latest = args.version or fetch_latest(os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN"))
    if not SEMVER.fullmatch(latest):
        raise SystemExit(f"refusing non-stable version {latest}")
    if latest == current:
        print(f"up to date: {current}")
        write_output(latest, current, False)
        return
    new_oauth = replace_oauth(oauth_text, latest)
    new_readme = replace_readme(readme_text, latest)
    if args.dry_run:
        print(f"would update {current} -> {latest}")
        write_output(latest, current, True)
        return
    with open(oauth_path, "w", encoding="utf-8") as handle:
        handle.write(new_oauth)
    with open(readme_path, "w", encoding="utf-8") as handle:
        handle.write(new_readme)
    print(f"updated {current} -> {latest}")
    write_output(latest, current, True)


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(f"error: {exc}", file=sys.stderr)
        sys.exit(1)
