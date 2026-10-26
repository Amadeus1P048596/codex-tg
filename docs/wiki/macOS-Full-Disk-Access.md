# macOS Full Disk Access deployment

Codex's `danger-full-access` disables its workspace sandbox. macOS privacy
controls (TCC) are separate. A repository, entitlement or installer cannot
export a person's local Full Disk Access grant to a fresh unmanaged Mac.

## Repeatable app installation

The macOS package installs:

- `/Applications/codex-tg.app`, bundle ID `tech.mideco.codex-tg.tray`;
- its daemon at `Contents/MacOS/ctr-go`, signing ID `tech.mideco.codex-tg.daemon`;
- `/usr/local/bin/ctr-go`, a compatibility wrapper that execs the bundled daemon.

Run `ctr-go service install` from this installation. Its LaunchAgent points to
the bundled executable and declares the associated app identity. This improves
privacy attribution; the association itself does not grant disk access.
Existing bare-binary or custom-launcher installations are not silently migrated.

On an unmanaged Mac, grant **System Settings > Privacy & Security > Full Disk
Access** to the packaged app once, then restart the service and test access from
an actual background Telegram task. Retain the app path, bundle IDs and signing
identity across upgrades. Permission retention should be verified after upgrading;
it is not guaranteed by a matching display name or path alone.

A current grant for a standalone binary is not automatically a grant for the new
app bundle. Plan that one-time migration while someone is at the Mac. Do not
remove or reset existing permissions just to install an update.

## Signed packages

On a Mac with a distribution signing identity installed in its keychain:

```bash
MACOS_CODESIGN_IDENTITY='Developer ID Application: Your Organization (TEAMID)' \
MACOS_INSTALLER_IDENTITY='Developer ID Installer: Your Organization (TEAMID)' \
VERSION=0.5.1 scripts/build-macos-pkg.sh
```

The builder signs the nested daemon first, then the app, and verifies the bundle.
The CLI always executes the bundled daemon. Signed apps are also retained under
`dist/apps/<arch>/codex-tg.app` for inspection and profile generation.
The installer identity is optional. Notarization is a separate release step;
signing alone does not claim notarization or guarantee Gatekeeper acceptance.

Without `MACOS_CODESIGN_IDENTITY`, the package uses ad-hoc development signing
and prints an explicit warning. Such builds cannot promise that a disk-access
grant survives recompilation. The default GitHub release workflow has no private
certificate and therefore remains a development-signing build until signing is
configured by the publisher. Keep certificates, private keys and passwords out
of source control.

## Managed Macs: preauthorize through MDM

For zero-touch permission rollout, an administrator must deliver a PPPC policy
through an approved MDM enrollment. Generate the policy from the signed app:

```bash
python3 scripts/generate-macos-pppc.py \
  --app dist/apps/arm64/codex-tg.app \
  --output dist/codex-tg-full-disk-access.mobileconfig
```

The generated profile authorizes **only Full Disk Access** for the app identity
and the installed daemon path, bound to their actual code-signing requirements.
It does not include Telegram/OpenAI credentials, local user IDs or a TCC database.
It refuses ad-hoc or unbound signing identities and refuses silent overwrites.
For a custom destination, pass `--install-path /absolute/path/codex-tg.app` so
build-staging paths never become deployment paths accidentally.

Deliver the profile and the correspondingly signed package with MDM. Merely
checking the profile into GitHub, bundling it in a pkg, double-clicking it, or
running `sudo` does not itself establish the required managed-device approval.
Profile generation is not evidence of successful MDM deployment: validate on a
test enrolled Mac with the real LaunchAgent and Codex child processes.

These settings do not preapprove Screen Recording, Accessibility, Automation,
Camera or Microphone. Files owned by another account or blocked by other access
controls can still be inaccessible.

## Checks

```bash
python3 scripts/test_macos_pppc.py
bash -n scripts/build-macos-pkg.sh
go test ./cmd/ctr-go
codesign --verify --deep --strict /Applications/codex-tg.app
```

The signing-aware generator deliberately fails for the default ad-hoc build.
No script in this workflow modifies TCC, disables system protections or installs
an MDM policy on the current Mac.

## Apple references

- [Privacy Preferences Policy Control payload](https://support.apple.com/guide/deployment/dep38df53c2a/web)
- [Apple DTS guidance on app-bundled daemons and Full Disk Access](https://developer.apple.com/forums/thread/758774?answerId=794648022)
- [Privacy & Security settings](https://support.apple.com/guide/mac-help/mchl211c911f/mac)
