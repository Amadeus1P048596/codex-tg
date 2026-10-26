# Hosted maintenance safety

A task that unloads its own LaunchAgent is killed before its cleanup can reload
the job. Protect the supported CLI maintenance entry points inside hosted tasks,
identify their environment explicitly and explain the boundary in runtime MCP
instructions. External restart must use launchd kickstart without bootout.

This is operational protection, not a replacement for an OS sandbox: a full-access
arbitrary script can bypass a CLI guard. Do not claim that shell guidance blocks
all direct launchctl or kill commands. No live destructive test should target
the production daemon.

Validate hosted mutation refusal before any launchctl/config mutation, preserve
status inspection and external maintenance, and prove failed kickstart leaves the
job registered. Existing migration scripts should be idempotent and not unload
their host. Review is performed as a self-review.
