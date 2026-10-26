# ADR-026: macOS Disk Access Packaging

- Status: accepted
- Amends ADR-018 macOS package layout and LaunchAgent attribution.

The packaged daemon lives inside the existing app bundle at a stable path, with
a stable signing identifier. The legacy CLI command remains an exec wrapper.
The generated LaunchAgent declares the associated app only for the known bundled
daemon layout. Standalone and custom-launcher installations remain unchanged.

Distribution builders may provide a signing identity. Sign the nested daemon
before the parent app and verify the result. Ad-hoc builds are explicitly
identified as development builds and make no permission-retention promise.

Provide an MDM PPPC generator bound to verified app/daemon signatures. It grants
only SystemPolicyAllFiles, uses the final installation path and rejects ad-hoc
code requirements. The generator emits a policy, never installs it. Normal
unmanaged deployment still needs local approval once; permission retention must
be verified across signed upgrades. Existing bare-binary grants are not migrated.

Do not export TCC databases, commit private certificates, bypass TCC, or claim
that an installer, entitlement, sudo or a repository can grant these permissions
on a fresh unmanaged machine.
