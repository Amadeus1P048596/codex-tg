# Approval routing and terminal timing repair

Goal: restarted App Servers may reuse JSON-RPC request IDs without routing an
approval to an older thread. Pending permission approvals produce a separate,
notifying Telegram message with routeable decision buttons. Repeated terminal
snapshots preserve the first terminal timestamp and never keep counting.

Scope: App Server request identity and timing normalization, approval persistence
and delivery, menu deployment configuration. Preserve manual command handlers.

Validation: request ID reuse across generations/clients, numeric RPC response ID,
stale response rejection, storage routing replacement, duplicate approval delivery,
terminal polling across all terminal states, targeted and complete Go checks,
local daemon deployment and Telegram menu readback. Review checks cover identity translation, stale-button rejection, routing
replacement and stable terminal timestamps.
