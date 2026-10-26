# macOS disk access deployment

Goal: ship a repeatable app identity and optional managed-device Full Disk Access
policy rather than copying a machine's private TCC database.

- Bundle the daemon inside the existing macOS app; retain the CLI command as an
  exec wrapper and point installed LaunchAgents to the bundled executable.
- Support inside-out signing with an operator-provided signing identity. Ad-hoc
  development builds must not claim durable permission retention.
- Generate PPPC profiles from verified distribution signatures for MDM delivery.
  Ordinary fresh Macs still need one local authorization.
- Preserve existing local installations and grants; no automatic service restart
  or TCC mutation is part of this repository change.

Validation: packaged app structure and LaunchAgent attribution, profile XML and
signature rejection, actual local pkg build and signature verification, existing
Go checks and publication secret scan. Review is performed as a self-review.
