#!/usr/bin/env python3
"""Explicitly opt a private codex-tg config into the access-token watchdog.

Does not install binaries, restart services, copy auth files, or refresh tokens.
"""
import argparse
import json
import os
from pathlib import Path
import re
import tempfile


def configure(config_path, source_path):
    config_path = Path(config_path).expanduser().resolve(strict=True)
    source_path = Path(source_path).expanduser().resolve(strict=True)
    if config_path == source_path:
        raise ValueError("config and credential source must be different files")
    if source_path.stat().st_size > 1024 * 1024:
        raise ValueError("credential source is too large")
    try:
        auth = json.loads(source_path.read_text())
        account = auth["tokens"]["account_id"]
        access = auth["tokens"]["access_token"]
        if not isinstance(account, str) or not account.strip() or not isinstance(access, str) or not access:
            raise ValueError()
    except (ValueError, KeyError, TypeError):
        raise ValueError("source must contain a ChatGPT account and access token") from None
    original = config_path.read_text()
    # Preserve all unrelated configuration verbatim; the daemon's config parser
    # handles quoting. JSON strings are compatible with its quoted-value parser.
    keys = ("CTR_GO_AUTH_SOURCE_FILE", "CTR_GO_AUTH_ACCOUNT_ID")
    lines = [line for line in original.splitlines()
             if not any(re.match(r"^\s*(?:export\s+)?" + key + r"\s*=", line) for key in keys)]
    lines.extend([keys[0] + "=" + json.dumps(str(source_path)), keys[1] + "=" + json.dumps(account)])
    data = "\n".join(lines) + "\n"
    fd, tmp = tempfile.mkstemp(prefix=".watchdog-", dir=config_path.parent)
    try:
        with os.fdopen(fd, "w") as f:
            os.fchmod(f.fileno(), 0o600)
            f.write(data)
            f.flush()
            os.fsync(f.fileno())
        os.replace(tmp, config_path)
    finally:
        if os.path.exists(tmp):
            os.unlink(tmp)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", required=True)
    parser.add_argument("--source", required=True)
    args = parser.parse_args()
    try:
        configure(args.config, args.source)
    except (OSError, ValueError):
        parser.exit(1, "Configuration failed; check config/source files and ChatGPT login.\n")
    print("Watchdog configured. Install the updated binary and restart externally after active tasks finish.")
    print("The source login owner must continue refreshing its own access token.")


if __name__ == "__main__":
    main()
