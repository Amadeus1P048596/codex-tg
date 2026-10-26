#!/usr/bin/env python3
"""Generate an MDM-only Full Disk Access policy from a distribution-signed app."""
import argparse
import plistlib
import re
import subprocess
import sys
import uuid
from pathlib import Path


def make_profile(bundle_id, app_requirement, daemon_requirement, install_path):
    for requirement in (app_requirement, daemon_requirement):
        if 'anchor apple generic' not in requirement or 'certificate leaf[subject.OU]' not in requirement:
            raise ValueError('A certificate-bound Apple code requirement is required; ad-hoc builds are unsupported')
    install = Path(install_path)
    if not install.is_absolute() or install.suffix != '.app':
        raise ValueError('Install path must be an absolute .app path')
    identifier = bundle_id + '.full-disk-access'
    def entry(identity, kind, requirement):
        return dict(Identifier=identity, IdentifierType=kind, CodeRequirement=requirement, Allowed=True, StaticCode=False)
    policy = dict(
        PayloadType='com.apple.TCC.configuration-profile-policy',
        PayloadVersion=1, PayloadIdentifier=identifier + '.policy',
        PayloadUUID=str(uuid.uuid5(uuid.NAMESPACE_URL, identifier + '.policy')),
        PayloadDisplayName='codex-tg Full Disk Access',
        Services={'SystemPolicyAllFiles': [
            entry(bundle_id, 'bundleID', app_requirement),
            entry(str(install / 'Contents/MacOS/ctr-go'), 'path', daemon_requirement),
        ]},
    )
    return dict(PayloadType='Configuration', PayloadVersion=1,
                PayloadIdentifier=identifier,
                PayloadUUID=str(uuid.uuid5(uuid.NAMESPACE_URL, identifier)),
                PayloadDisplayName='codex-tg Full Disk Access (MDM)',
                PayloadDescription='Deploy using approved device management. This file does not grant access by itself.',
                PayloadScope='System', PayloadContent=[policy])


def signature(target):
    subprocess.run(['codesign', '--verify', '--strict', str(target)], check=True, capture_output=True)
    details = subprocess.run(['codesign', '-dv', '--verbose=2', str(target)], check=True, capture_output=True, text=True).stderr
    team = re.search(r'^TeamIdentifier=(.+)$', details, re.M)
    if 'Signature=adhoc' in details or not team or team.group(1) == 'not set':
        raise ValueError('Sign the app and daemon with a distribution identity before generating an MDM profile')
    result = subprocess.run(['codesign', '-d', '-r-', str(target)], check=True, capture_output=True, text=True)
    match = re.search(r'^designated => (.+)$', result.stdout + '\n' + result.stderr, re.M)
    if not match:
        raise ValueError('No designated code requirement found')
    return team.group(1), match.group(1)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--app', required=True, type=Path, help='Built, distribution-signed codex-tg.app')
    parser.add_argument('--install-path', default='/Applications/codex-tg.app', help='Final deployment path, not a build staging directory')
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    app = args.app.resolve()
    with (app / 'Contents/Info.plist').open('rb') as stream:
        info = plistlib.load(stream)
    app_team, app_requirement = signature(app)
    daemon_team, daemon_requirement = signature(app / 'Contents/MacOS/ctr-go')
    if app_team != daemon_team:
        raise ValueError('App and daemon must use the same signing team')
    profile = make_profile(info['CFBundleIdentifier'], app_requirement, daemon_requirement, args.install_path)
    # Refuse to overwrite an existing policy silently.
    with args.output.open('xb') as stream:
        plistlib.dump(profile, stream)
    print('Wrote MDM policy:', args.output)
    print('Send this policy through approved MDM. A new unmanaged Mac still requires local approval.')


if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError, subprocess.CalledProcessError) as exc:
        sys.exit(str(exc))
