# Regression Map

This map is the handoff index for agents changing Codex control-plane
contracts, Telegram routing, observer panels, lifecycle recovery, diagnostics,
or Plan Mode.

When behavior changes, update the relevant ADR first, then update or add the tests named here. The tests are part of the architecture: they describe the contract that must survive App Server drift and daemon restarts.

## Control Plane Architecture

ADR: `docs/adr/ADR-019-codex-control-plane.md`; feature brief is
`docs/process/v0.5.0-codex-control-plane-brief.md`; roadmap is
`docs/process/v0.5.0-control-api-roadmap.md`.

Future primary tests:

- App Server capability-map smoke detects supported and missing schema methods.
- Control-core thread lifecycle tests cover list/read/start/resume/fork/rename
  through an App Server-backed implementation.
- Control-core turn lifecycle tests cover start/steer/interrupt plus stale-active
  recovery preservation.
- Event normalization tests cover lifecycle/tool/final/approval/input events
  before Telegram-specific rendering.
- Notification policy tests classify normalized events as `urgent`, `normal`,
  `silent`, or `digest`.
- Telegram adapter compatibility tests prove existing commands and callback
  routing still work on top of the control API.

Contract notes:

- App Server remains authoritative for live interactive Codex state.
- SDK/MCP adapters are allowed only as orchestration adapters under ADR-019.
- Session JSONL must not re-enter live UI/control state.
- Generated App Server schema is an input to capability detection, not a file to
  commit as a runtime source of truth.
- Telegram is a channel adapter; Telegram ids must not become Codex identity.

## Distribution And Local Config

ADR: `docs/adr/ADR-017-release-binaries-and-init.md` and
`docs/adr/ADR-018-macos-service-installer.md`; feature briefs are
`docs/process/v0.3.0-distribution-brief.md` and
`docs/process/v0.4.0-macos-service-installer-brief.md`.

Primary tests:

- `internal/config/config_test.go::TestParseEnvFileSupportsCommentsAndQuotes`
- `internal/config/config_test.go::TestParseEnvFileRejectsInvalidLine`
- `internal/config/config_test.go::TestParseEnvFilePreservesQuotedWindowsPath`
- `internal/config/config_test.go::TestParseEnvFileDecodesStrconvQuotedWindowsPath`
- `internal/config/config_test.go::TestLoadReadsConfigFileAndEnvOverridesIt`
- `internal/config/config_test.go::TestLoadAppliesRuntimeProxyEnvFromConfigFile`
- `internal/config/config_test.go::TestLoadDoesNotOverrideExplicitRuntimeProxyEnv`
- `cmd/ctr-go/main_test.go::TestRunInitWritesPrivateConfigAndRefusesOverwrite`
- `cmd/ctr-go/main_test.go::TestRunAutomationMCPUsesConfiguredNativeStore`
- `cmd/ctr-go/main_test.go::TestRunInitForceOverwritesConfig`
- `cmd/ctr-go/main_test.go::TestStatusAndDoctorDoNotLeakConfigFileToken`
- `cmd/ctr-go/main_test.go::TestFatalErrorSanitizerRedactsTelegramBotURL`
- `cmd/ctr-go/service_test.go::TestServiceInstallNonInteractiveWritesConfigAndLaunchAgent`
- `cmd/ctr-go/service_test.go::TestServiceInstallCapturesRuntimeProxyEnvInConfig`
- `cmd/ctr-go/service_test.go::TestServiceInstallInteractiveWizardRetriesInvalidValues`
- `cmd/ctr-go/service_test.go::TestServiceInstallNonInteractiveReportsMissingFlags`
- `cmd/ctr-go/service_test.go::TestRenderLaunchAgentPlistContainsOnlyConfigEnvironment`
- `cmd/ctr-go/service_test.go::TestServiceLifecycleUsesLaunchctlRunner`
- `cmd/ctr-go/service_test.go::TestServiceStartAcceptsKickstartFailureWhenServiceLoaded`
- `internal/trayapp/actions_test.go::TestCTRGoArgs`
- `internal/trayapp/actions_test.go::TestServiceSetupArgs`
- `internal/appserver/client_test.go::TestBuildCommandScopesCodexHomeToAppServer`

Contract notes:

- `config.env` is local runtime state and must not be committed.
- Explicit environment variables override config file values.
- `CTR_GO_CODEX_HOME` is applied only to spawned App Server children; it must
  not mutate the parent process environment.
- Client-private Codex runtime SQLite/session state must never be linked between
  Desktop and Telegram homes. Shared static resources and an independent durable
  memory store are allowed.
- macOS LaunchAgent plists must carry only `CTR_GO_CONFIG`, never token/user/cwd env values.
- Proxy env needed by the operator shell may be stored in the private config and
  applied by the process after startup.
- Tray control is not a settings editor in v0.4.0.
- Release archives and packages must not include local config, sessions, SQLite state, logs, or screenshots.

## Telegram-Native Scheduled Tasks

ADR: `docs/adr/ADR-023-telegram-native-scheduled-tasks.md`; feature brief is
`docs/process/telegram-scheduled-tasks-brief.md`.

Primary tests:

- `internal/automation/store_test.go::TestStoreCRUDPreservesUnknownFields`
- `internal/automation/store_test.go::TestStoreRejectsUnsafeOrUnsupportedAutomation`
- `internal/automation/store_test.go::TestStoreRequiresNativeCronRuntimeFields`
- `internal/automation/store_test.go::TestStoreHidesAndProtectsDesktopHeartbeatTasks`
- `internal/automation/store_test.go::TestStoreMarksTasksAsTelegramOwnedAndPersistsCWD`
- `internal/automation/scheduler_test.go::TestLatestDueUsesLocalWallClockAndTaskUpdateBoundary`
- `internal/automation/scheduler_test.go::TestLatestDueSupportsHourlyAndWeeklyIntervals`
- `internal/automation/mcp_test.go::TestServeMCPListsAndCallsAutomationTool`
- `internal/automation/mcp_test.go::TestServeMCPReturnsToolErrorsAsMCPContent`
- `internal/appserver/client_test.go::TestAutomationMCPConfigUsesExplicitBinaryAndDirectory`
- `internal/appserver/client_test.go::TestAutomationMCPConfigIsInjectedIntoThreadStartAndResume`
- `internal/appserver/client_test.go::TestAutomationMCPConfigIsDisabledWithoutDirectory`
- `internal/config/config_test.go::TestLoadReadsConfigFileAndEnvOverridesIt`
- `internal/config/config_test.go::TestFromEnvDefaultsAutomationsInsideTelegramHome`
- `internal/config/config_test.go::TestIsDesktopAutomationsDirRejectsConventionalSharedStore`
- `internal/daemon/service_test.go::TestAutomationTickStartsTelegramBackgroundTurnOnlyOnce`
- `internal/daemon/service_test.go::TestNewRejectsConventionalDesktopAutomationStore`
- `internal/daemon/service_test.go::TestAutomationRunStateFollowsTerminalAppServerSnapshot`
- `cmd/ctr-go/main_test.go::TestRunInitWritesPrivateConfigAndRefusesOverwrite`
- `cmd/ctr-go/service_test.go::TestServiceInstallInteractiveWizardRetriesInvalidValues`

Contract notes:

- App Server still owns Telegram interactive thread state. The injected MCP
  server is a narrow orchestration adapter under ADR-019.
- `CTR_GO_AUTOMATIONS_DIR` is TG-private and defaults under `CTR_GO_HOME`;
  Desktop and Telegram do not share task definitions or schedulers.
- Both `thread/start` and `thread/resume` must receive the MCP config so existing
  Telegram threads gain the tool without rollout rewrites.
- Only standalone `cron` tasks are accepted. Heartbeat continuation is invalid.
- The daemon owns due-slot calculation and an at-most-once SQLite claim; App
  Server owns the new thread and turn after dispatch.
- Live operator QA must confirm a real due occurrence starts in the TG runtime
  and produces Telegram readback without appearing in the Desktop task store.

## Plan Mode Routing

ADR: `docs/adr/ADR-006-plan-prompt-mode.md`; reset addendum:
`docs/adr/ADR-016-plan-mode-reset-contract.md`

Primary tests:

- `internal/daemon/service_test.go::TestPlanCommandStartsPlanCollaborationMode`
- `internal/daemon/service_test.go::TestPlanCommandUsesBoundThreadWhenNoExplicitThread`
- `internal/daemon/service_test.go::TestPlanCommandUnknownHeadUsesBoundThreadAsPromptText`
- `internal/daemon/service_test.go::TestPlanCommandUnknownHeadWithoutImplicitRouteShowsUsage`
- `internal/daemon/service_test.go::TestPlanCommandUUIDLikeHeadStaysExplicit`
- `internal/daemon/service_test.go::TestPlanCommandKnownThreadHeadStaysExplicit`
- `internal/daemon/service_test.go::TestReplyPlanFlagStartsPlanCollaborationMode`
- `internal/daemon/service_test.go::TestReplyDefaultFlagStartsDefaultCollaborationMode`
- `internal/daemon/service_test.go::TestDefaultModeCommandStartsDefaultCollaborationMode`
- `internal/daemon/service_test.go::TestPlanFinalCardShowsTurnOffPlanButton`
- `internal/daemon/service_test.go::TestNormalFinalCardDoesNotShowTurnOffPlanButton`
- `internal/daemon/service_test.go::TestReplyCommandConsumesDefaultOverrideOnce`
- `internal/daemon/service_test.go::TestDefaultOverrideSurvivesTurnStartFailure`
- `internal/daemon/service_test.go::TestPlanCommandClearsStaleDefaultOverride`
- `internal/daemon/service_test.go::TestStopSetsDefaultOverrideForActiveThread`
- `internal/daemon/service_test.go::TestStopSetsDefaultOverrideForIdleThread`
- `internal/daemon/observer_ui_v2_test.go::TestTurnOffPlanCallbackSetsDefaultOverrideAndEditsFinalCard`
- `internal/daemon/observer_ui_v2_test.go::TestTurnOffPlanCallbackRejectsMismatchedMessageID`
- `internal/daemon/service_test.go::TestPlanModeCommandCanRouteByReply`
- `internal/daemon/service_test.go::TestPlainReplyToSyntheticPlanPromptUsesTurnSteer`
- `internal/daemon/service_test.go::TestPlainReplyToSyntheticPlanPromptFallsBackToTurnStart`
- `internal/daemon/service_test.go::TestPlainReplyToRealPlanPromptUsesServerRequest`
- `internal/daemon/observer_ui_v2_test.go::TestSyncThreadPanelCreatesRouteablePlanPromptAndDedupes`
- `internal/daemon/observer_ui_v2_test.go::TestSyncThreadPanelCreatesServerRequestPlanPromptRoute`

Live E2E:

- `tests/live_e2e/telegram_readback_e2e.py` case `plan_mode_reset`

Contract notes:

- `/plan <text>` uses reply, armed state, or bound thread routing.
- `/plan <thread> <text>` is explicit only for known or UUID-like thread ids.
- `/reply --plan <thread> <text>` remains strict.
- `Turn off Plan` on a Plan Final Card and `/stop <thread>` set a one-shot Default override for the next ordinary turn; they do not start a reset turn.
- The one-shot override is cleared after a successful ordinary `turn/start` and remains after a failed `turn/start`.
- Hidden `/default` and `/reply --default` fallback paths remain tested but are not advertised in public help or Telegram command menu.
- `Turn off Plan` is panel-bound like Details; stale panel/message/thread/turn callbacks fail closed without changing state.
- Plan choice buttons must stay scoped to the same turn as the `[Plan]` card.
- Stale pending `user_input` from an older turn must not add `answer_choice` buttons to a newer `[commentary]` panel.

## Project Thread Creation

ADR: `docs/adr/ADR-014-newchat-chat-folder-contract.md`; feature brief is `docs/process/create-thread-from-project-brief.md`.

Primary tests:

- `internal/daemon/service_test.go::TestProjectsCommandShowsProjectButtonsGroupedByCWD`
- `internal/daemon/service_test.go::TestIsCodexChatsCWDMatchesGenericMacAndWindowsPaths`
- `internal/daemon/service_test.go::TestProjectsCommandShowsChatsSectionAndSortsByRecency`
- `internal/daemon/service_test.go::TestProjectsPaginationUsesPreviewLimitsAndKeepsLatestChats`
- `internal/daemon/service_test.go::TestOpenChatsPaginatesAndChatSelectionBindsThread`
- `internal/daemon/service_test.go::TestProjectsCloseDeletesMenuMessage`
- `internal/daemon/service_test.go::TestProjectOpenShowsNewThreadMenu`
- `internal/daemon/service_test.go::TestProjectNewThreadArmsThenPlainTextCreatesThread`
- `internal/daemon/service_test.go::TestProjectNewThreadRejectsThreadStartWithoutID`
- `internal/daemon/service_test.go::TestProjectNewThreadTurnStartFailureSavesThread`
- `internal/daemon/service_test.go::TestNewChatCommandCreatesCodexUIChatCWDAndBinds`
- `internal/daemon/service_test.go::TestNewChatCommandWithoutPromptCollectsTitleThenPrompt`
- `internal/daemon/service_test.go::TestNewChatCWDUsesFallbackSlugAndCollisionSuffix`
- `internal/daemon/service_test.go::TestNewThreadCommandCreatesThreadWithoutCWDAndBinds`
- `internal/daemon/service_test.go::TestNewThreadCommandWithoutPromptCollectsTitleThenPrompt`
- `internal/daemon/service_test.go::TestCancelClearsPendingNewChatOrNewThreadPrompt`
- `internal/daemon/service_test.go::TestPendingNewChatPromptExpiresAndDoesNotStartThread`
- `internal/daemon/service_test.go::TestPendingNewThreadPromptSurvivesServiceRestart`
- `internal/daemon/service_test.go::TestInlineNewThreadCommandClearsOlderPendingCreation`
- `internal/daemon/service_test.go::TestNewChatCommandRejectsMissingThreadID`
- `internal/daemon/service_test.go::TestNewChatCommandTurnStartFailureSavesAndBindsThread`
- `internal/daemon/service_test.go::TestNewThreadCommandTurnStartFailureSavesAndBindsThread`
- `internal/config/config_test.go::TestFromEnvReadsCodexChatsRoot`
- `internal/telegram/bot_test.go::TestDefaultCommandsExposeNewChatMenuCommand`
- `internal/daemon/service_test.go::TestSummaryPanelDoesNotShowStalePendingUserInputButtons`
- `tests/config_env_test.go::TestFromEnvProjectChatLimitsClampInvalidValues`

Live E2E:

- Open `/projects`, choose a project, press `New thread`, send a prompt, and verify a new thread/run reaches `[Final]`.
- Open `/projects`, verify normal projects are sorted by recent activity, `Documents/Codex` threads are shown only as latest Chat previews, then open full `Chats` pagination and select a Chat.
- Run `/newchat`, enter a title, then enter a distinct prompt. Verify the App Server title matches the first input, only the second input starts the turn, the title-derived cwd exists under the configured Chats root, the thread reaches its Completed card, `/projects -> Open Chats` shows it, and a plain follow-up routes to the newly bound Chat thread.
- Repeat the interactive flow with a captioned photo as the second input. Verify
  the new thread receives both text and `localImage`, and the previous binding
  receives neither. Send a photo during the title stage and verify the title
  prompt remains armed without routing the photo to the previous binding.
- Run `/newthread`, enter a title and then a distinct prompt. Verify the title is written through, only the prompt starts the turn, and no Chat cwd is created under the configured Chats root.
- Arm either title-then-prompt flow, send `/cancel` at either stage, and verify the next plain message is not consumed as creation input. Repeat across a daemon restart and after the 15-minute expiry boundary.
- Run the one-line `/newchat <prompt>` and `/newthread <prompt>` forms and verify backward compatibility.
- Send a plain reply after creation and verify it routes to the newly bound thread.
- Run a Plan Mode prompt with structured choices and verify choice buttons appear only on the current `[Plan]` card.

Contract notes:

- Project/workspace identity comes from cached thread `cwd`; this flow does not create or edit work directories.
- Threads under generic `Documents/Codex` cwd roots or configured `CTR_GO_CODEX_CHATS_ROOT` are Codex UI `Chats`, not normal projects. A Chat selection opens and binds its single thread; Chat lists do not expose project `New thread`.
- Main `/projects` uses project pagination with configurable preview limits and keeps latest Chat previews newest-first. Full Chat pagination lives behind `Open Chats`.
- Project buttons use `N. Project name`; Chat buttons use `Chat N. Thread name`. The menu must not render internal `key:` rows and must show each project row's `last thread:`.
- `/newchat <prompt>` creates a dated Chat cwd from a prompt slug and passes that cwd to App Server `thread/start`; it remains the backward-compatible one-line form.
- `/newthread <prompt>` creates a new App Server thread without a Telegram-selected cwd parameter. It must not create a Chat folder; App Server may still attach the daemon default cwd.
- `/newchat` and `/newthread` without prompts collect an explicit plain-text
  title and then a distinct first prompt, which may contain Telegram
  `localImage` inputs. The pending stage, title, and context are SQLite-backed,
  survive restart, expire after 15 minutes, and `/cancel` deletes them. A photo
  during the title stage stays out of the previous binding. The title is written
  to App Server and retained as user-owned Telegram metadata.
- A one-line `/newchat <prompt>` or `/newthread <prompt>` clears any older pending creation state before starting immediately.
- Telegram must not accept arbitrary local filesystem paths for thread creation.
- The first prompt is required; create-only threads are out of scope for this slice.

## Full Thread ID Access

ADR: `docs/adr/ADR-007-parallel-thread-visual-identity.md`

Primary tests:

- `internal/daemon/service_test.go::TestContextCardBoundThreadIncludesFullThreadID`
- `internal/daemon/service_test.go::TestSummaryPanelGetThreadIDButtonSendsCopyableIDs`
- `internal/daemon/service_test.go::TestFinalSummaryPanelHasGetThreadIDButton`
- `internal/daemon/service_test.go::TestFinalCardGetThreadIDButtonSendsCopyableIDs`

Contract notes:

- Header chips like `T:d663` and `R:d9bc` are visual hints only.
- Operators must be able to retrieve copyable full ids from Telegram without SQLite/log access.
- The default activity card emphasizes the conversation marker, Codex, textual
  state, result, and timing. Short ids remain secondary code-styled metadata.

Primary rendering tests:

- `internal/daemon/visual_identity_test.go::TestVisualCardHeaderPrioritizesTitleRoleStatusAndTiming`
- `internal/daemon/observer_ui_v2_test.go::TestUserAndFinalCardsEmphasizeIdentityStatusAndTiming`
- `internal/daemon/observer_ui_v2_test.go::TestRenderSummaryPanelShowsActiveRunElapsedTimeInHeader`

## Final Card Details Binding

ADR: `docs/adr/ADR-004-final-card-details-ux.md`

Primary tests:

- `internal/daemon/observer_ui_v2_test.go::TestFinalCardDetailsCallbacksEditSameMessageAndExportToolsFile`
- `internal/daemon/observer_ui_v2_test.go::TestFinalCardDetailsShowsToolOnlyTurnWithoutCommentary`
- `internal/daemon/observer_ui_v2_test.go::TestDetailsCallbacksUsePanelTurnInsteadOfLatestThreadTurn`
- `internal/daemon/observer_ui_v2_test.go::TestDetailsCallbacksStayBoundToOriginalPanelAfterNewerRunCompletes`
- `internal/daemon/observer_ui_v2_test.go::TestDetailsCallbackWithoutPanelIDDoesNotFallbackToCurrentPanel`
- `internal/daemon/observer_ui_v2_test.go::TestDetailsCallbackRejectsMismatchedMessageID`
- `internal/daemon/observer_ui_v2_test.go::TestDetailsToolsFileRejectsMismatchedPanelRoute`

Contract notes:

- `Details`, pagination, `Tool on`, `Tools file`, and `Back` are bound to the completed panel/card that produced the callback.
- A Details callback without a valid `panel_id`, with a mismatched thread/turn, or from another Telegram message is stale and must not edit/export current run data.
- Pressing `Back` on an older completed run must restore that older Final Card in the same message, not duplicate or replace it with the latest run.
- Finalization edits the Working card in place. Details/Back stay bound to that
  same summary message id and must never target a newer turn.
- Tool-only turns with no commentary and empty output still expose completed command/status in Details, Tool mode, and Tools file under `Tool activity`.

## Telegram Activity Card And Notification Contract

ADR: `docs/adr/ADR-015-telegram-notification-contract.md` and
`docs/adr/ADR-021-telegram-activity-card-lifecycle.md`; feature brief is
`docs/process/telegram-output-style-v2-brief.md`.

Primary tests:

- `internal/daemon/activity_aggregator_test.go`
- `internal/daemon/activity_lifecycle_test.go`
- `internal/daemon/activity_lifecycle_test.go::TestShortFinalCompletesWorkingCardAndSendsAudibleNotice`
- `internal/daemon/notification_style_test.go`
- `internal/tgformat/html_test.go`
- `internal/telegram/api_test.go::TestClientSendMessageSilentSetsDisableNotification`
- `internal/telegram/api_test.go::TestClientSendChatActionTargetsTopic`
- `internal/telegram/api_test.go::TestClientGetsAndDownloadsTelegramFileWithLimit`
- `internal/telegram/api_test.go::TestClientSendDocumentSilentSetsDisableNotification`
- `internal/telegram/bot_test.go::TestBotDeliverDirectResponseSendsSilentMessage`
- `internal/telegram/bot_test.go::TestLargestTelegramPhotoSelectsHighestResolution`
- `internal/appserver/client_test.go::TestTurnStartInputParamsIncludesTextAndLocalImage`
- `internal/daemon/service_test.go::TestHandleMessageWithLocalImageStartsRichTurn`
- `internal/daemon/service_test.go::TestPendingNewChatConsumesCaptionAndLocalImageAsFirstPrompt`
- `internal/daemon/service_test.go::TestPendingNewChatPhotoWhileWaitingForTitleDoesNotRouteToBinding`
- `internal/config/config_test.go::TestMarshalJSONIncludesNotifyNewRun`
- `tests/config_env_test.go::TestFromEnvDefaultsLoggingOn`
- `tests/config_env_test.go::TestFromEnvPrefersGoScopedEnvVars`
- `internal/daemon/observer_ui_v2_test.go::TestSyncThreadPanelCreatesRouteablePlanPromptAndDedupes`
- `internal/daemon/observer_ui_v2_test.go::TestFinalCardDetailsCallbacksEditSameMessageAndExportToolsFile`
- `internal/daemon/service_test.go::TestThreadsCommandUsesPreviewForPlaceholderTitleAndUnicodeSafeButtons`
- `internal/daemon/service_test.go::TestThreadsCommandDoesNotExposeCachedThreadsMissingFromCurrentRuntime`
- `internal/daemon/service_test.go::TestShowThreadRejectsCachedThreadMissingFromCurrentRuntime`
- `internal/daemon/foreground_session_test.go::TestBackgroundThreadSuppressesProgressAndSendsOneCompletionNotice`
- `internal/daemon/foreground_session_test.go::TestBackgroundThreadSendsOneNeedsInputNoticeWithoutShowingProgressCard`
- `internal/daemon/foreground_session_test.go::TestSwitchThreadHidesPreviousWorkingCardAndShowsSelectedCard`
- `internal/daemon/thread_admin_test.go::TestNewThreadUsesPromptTitleWhileAppServerTitleIsPlaceholder`
- `internal/daemon/thread_admin_test.go::TestNewThreadBecomesTheForegroundSession`
- `internal/daemon/thread_admin_test.go::TestSyncThreadsPreservesPromptTitleUntilRuntimePublishesRealTitle`
- `internal/daemon/thread_admin_test.go::TestTitleCommandEditsCurrentActivityCardInPlace`
- `internal/daemon/thread_admin_test.go::TestCurrentCommandShowsForegroundThreadTitleStatusAndShortID`
- `internal/daemon/thread_admin_test.go::TestArchiveCommandRequiresConfirmationThenArchivesCurrentThread`
- `internal/daemon/thread_admin_test.go::TestArchiveCommandBlocksRunningOrWaitingThread`
- `internal/daemon/thread_admin_test.go::TestArchiveResumedThreadRecyclesIdleLiveSessionAndRestoresOtherWriters`
- `internal/daemon/thread_admin_test.go::TestArchiveResumedThreadDoesNotRecycleWhileAnotherWriterIsActive`
- `internal/daemon/thread_admin_test.go::TestArchiveConfirmationCanBeCancelledWithoutArchiving`
- `internal/appserver/thread_archive_compat_test.go::TestThreadArchivePreparesWindowsStateBeforeRPC`
- `internal/appserver/thread_archive_compat_test.go::TestThreadResumePreparesWindowsStateBeforeRPC`
- `internal/appserver/thread_archive_compat_test.go::TestPrepareThreadRolloutStateNormalizesWindowsExtendedPath`
- `internal/appserver/thread_archive_compat_test.go::TestThreadArchiveFreshSessionCacheResetsWithGeneration`
- `internal/daemon/thread_admin_test.go::TestUnarchiveListsTenPerPageAndRestoresClickedThread`

Live E2E:

- `go test -tags live_e2e ./internal/appserver -run LiveWindowsThreadArchiveCompat`
  creates an isolated persisted thread, injects the affected Windows path
  prefix, resumes it into App Server's in-memory state, and verifies that the
  real App Server archives it successfully.
- `internal/daemon/thread_navigation_test.go::TestHomeShowsCurrentSessionAndPersistentInboxCount`
- `internal/daemon/thread_navigation_test.go::TestHomeShowsEveryConcurrentRunningSessionStatus`
- `internal/daemon/thread_navigation_test.go::TestHomeBoundsConcurrentRunningSessionDetails`
- `internal/daemon/thread_navigation_test.go::TestStartOpensTheSessionHome`
- `internal/daemon/thread_navigation_test.go::TestHomeNewSessionChooserArmsTwoStepPromptInPlace`
- `internal/daemon/thread_navigation_test.go::TestInboxPersistsBackgroundAttentionAndSwitchClearsIt`
- `internal/daemon/thread_navigation_test.go::TestPrimaryActivityCardUsesChineseStatusAndLabels`

Live E2E:

- Run a sub-four-second turn and verify typing followed by one terminal card.
- Run a slow tool sequence and verify one Working message is edited no more than
  once every four seconds, becomes Completed in place, and produces one compact
  audible completion notice.
- Verify raw `[Tool]`, `[Output]`, `Last completed tool`, and empty-output labels
  never appear in the default card.
- Send a photo with and without a caption to a bound thread and verify the image
  reaches Codex and follows the same one-card lifecycle.
- After entering an interactive `/newchat` title, send a captioned photo as the
  first prompt and verify it creates the new Chat rather than using the prior
  binding.
- Run a Plan Mode structured-choice prompt and verify the summary card becomes
  Needs input with the correct answer buttons.

Contract notes:

- Activity cards render the thread/task title as the primary first row, with
  the conversation marker before it and Codex/state/duration on the second row.
- Missing titles use the compact one-row status fallback and must not render an
  empty bold element.
- The first four seconds use Telegram typing only; a typing API failure creates
  the Working card immediately.
- One turn normally owns one message. Active edits have a four-second floor;
  terminal and input-required transitions bypass it.
- A short foreground terminal transition sends one de-duplicated compact audible
  notice because Telegram does not notify for edits. Fast terminal cards and
  separately sent long results are already audible and do not add that notice.
- The leading emoji is conversation identity. Status is text, never a replacement emoji.
- Raw tool events are aggregated, de-duplicated, prioritized, and reduced to at
  most three activities plus one compact current command.
- Short finals edit the activity card in place. Long finals complete that card
  and may continue in separate `Codex · Result` messages.
- Photo input uses Telegram `getFile` plus App Server `localImage`; no photo
  receipt or User card is created.
- A successfully dispatched photo remains on disk for the bounded App Server
  asynchronous-read window, then scheduled cleanup removes it; stale cleanup
  touches only expired `telegram-photo-*` inputs.
- Markdown pipe tables outside fenced code become labeled Telegram list
  records in both entity and HTML render paths: first-column values are record
  titles and remaining columns are labeled fields. Fenced examples stay literal.
- Explicit exports and direct command/menu responses are silent.
- Legacy Tool/Output views are diagnostic drill-down surfaces only.
- One chat/topic has one foreground thread. Background progress produces no
  Working cards; terminal and needs-input states produce one de-duplicated
  switch notice.
- Switching deletes the previous non-terminal foreground card and displays the
  selected thread's current card.
- `/threads` uses only the current Telegram runtime's thread list and renders
  session titles as Unicode-safe inline buttons. Stale cached Desktop sessions
  must not be listed or opened.
- Interactive `/newchat` and `/newthread` titles are written through and retained
  as user-owned metadata. UUID and generic new-thread titles do not overwrite a
  prompt-derived fallback for legacy one-line creation; a real App Server title may replace an automatic fallback. `/title` writes
  through, marks the title as user-owned, and updates the current card without
  creating a replacement card.
- `/current` shows only the foreground session with short metadata. `/archive`
  requires confirmation and rejects a stale confirmation after focus changes.
  Active and input-waiting sessions cannot be archived. `/unarchive` uses App
  Server's archived filter, ten-row cursor pages, title buttons, and in-place
  restore results with switch/continue actions.
- `/start` and `/home` render the session hub. Home excludes the foreground from
  its background-running count, rejects stale terminal snapshots, expands at
  most five running rows with direct view actions, and summarizes any remainder.
  `/inbox` persists background terminal/input attention, and switching clears
  the selected item.
- Primary status and button copy is Chinese while command names and internal
  protocol state remain stable.

## Turn Lifecycle And Stale Active Recovery

ADR: `docs/adr/ADR-012-turn-lifecycle-normalization.md`

Primary tests:

- `internal/appserver/normalize_test.go::TestSnapshotFromThreadReadTreatsFinalAnswerAsCompletedWhenStatusIsStale`
- `internal/daemon/service_test.go::TestStaleActiveThreadWithFinalAnswerStartsNewTurn`
- `internal/daemon/service_test.go::TestNoActiveTurnSteerFailureFallsBackToTurnStart`
- `internal/daemon/service_test.go::TestReplyToActiveThreadDoesNotFallbackToTurnStartWhenSteerFails`
- `internal/daemon/service_test.go::TestReplyToActiveThreadSteersActiveTurn`
- `internal/daemon/observer_ui_v2_test.go::TestGlobalObserverDoesNotRecreateTelegramOriginPanelOnEditFailure`

Contract notes:

- A final answer is terminal evidence unless the turn is waiting for approval or user input.
- `no active turn to steer` means stale active state and may fall back to a new turn after re-read.
- Active or not-steerable failures still block fallback `turn/start`.
- A Telegram-origin panel must not be duplicated by global observer sync for the same marked turn.

## App Server Session Lifecycle

ADR: `docs/adr/ADR-012-turn-lifecycle-normalization.md` and
`docs/adr/ADR-020-telegram-writer-handoff.md`; feature brief is
`docs/process/telegram-writer-handoff-brief.md`.

Primary tests:

- `internal/daemon/service_test.go::TestEnsureLiveSessionSerializedAgainstReconcile`
- `internal/daemon/service_test.go::TestRepairInvalidatesOldLiveLoop`
- `internal/daemon/service_test.go::TestControlLoopProcessesRepairBeforeReconcile`
- `internal/daemon/service_test.go::TestBootstrapTrackedStateResumesBoundThreadByDefault`
- `internal/daemon/service_test.go::TestBootstrapTrackedStateSkipsManuallyReleasedBoundThread`
- `internal/daemon/service_test.go::TestBindHereAcquiresWriterAndShowsReleaseButton`
- `internal/daemon/service_test.go::TestBindHereKeepsRouteAndReportsAnotherWriterConflict`
- `internal/daemon/service_test.go::TestReleaseWriterCallbackPersistsReleaseAndRecyclesLiveSession`
- `internal/daemon/service_test.go::TestSendInputWriterConflictReturnsDirectResponseWithoutQueue`
- `internal/daemon/service_test.go::TestReleaseTelegramWritersRefusesActiveOwnedThread`
- `internal/daemon/service_test.go::TestReleaseTelegramWritersRefusesUnverifiableOwnedThread`
- `internal/daemon/service_test.go::TestReleaseTelegramWritersAllowsTerminalTurnWithStaleThreadStatus`
- `internal/daemon/service_test.go::TestReleaseTelegramWritersRecyclesOnlyIdleLiveSession`
- `internal/daemon/service_test.go::TestAutoReleaseTelegramWritersAfterFiveMinutesIdle`
- `internal/daemon/service_test.go::TestAutoReleaseTelegramWritersWaitsForActiveTurn`
- `internal/telegram/bot_test.go::TestDefaultCommandsExposeNewChatMenuCommand`
- `internal/appserver/client_test.go::TestClientStartConcurrentCallsShareInitializedSession`
- `internal/appserver/client_test.go::TestClientStartFailureLeavesClientRetryable`

Contract notes:

- One live App Server session has one live event loop per generation.
- Stale old live-loop closes must not clear newer session state.
- Repair is serialized with reconcile/startup and is processed before replacement reconcile.
- Bind acquires a writer; repair/restart reacquire bound writers unless manually released, while observer-only tracking stays read-only.
- Explicit Telegram writes acquire a writer at dispatch time; unexpected writer
  conflicts from another connection in the isolated runtime fail immediately
  without queueing or parallel turns.
- `/release` and the session-card release button fail closed for active or unverifiable Telegram-owned threads, persist release markers, and otherwise replace only the live session.

## Transient Interrupted Gating

ADR: `docs/adr/ADR-012-turn-lifecycle-normalization.md`

Primary tests:

- `internal/daemon/terminal_gate_test.go::TestTelegramEmptyInterruptedGateDefersAndKeepsHotPollingMetadata`
- `internal/daemon/terminal_gate_test.go::TestTelegramEmptyInterruptedGateRecoversAndClearsDefer`
- `internal/daemon/terminal_gate_test.go::TestTelegramEmptyInterruptedGateGraceExpiryAccepts`
- `internal/daemon/terminal_gate_test.go::TestTelegramEmptyInterruptedGateExplicitInterruptBypassesDefer`
- `internal/daemon/terminal_gate_test.go::TestTelegramFinalInterruptedGateDefersUntilRecovered`
- `internal/daemon/terminal_gate_test.go::TestTelegramPartialInterruptedGateDefersUntilFinalOrGrace`
- `internal/daemon/service_test.go::TestPollTrackedDefersTelegramOriginEmptyInterruptedAndKeepsActiveState`
- `internal/daemon/service_test.go::TestPollTrackedDefersTelegramOriginPartialInterruptedAndKeepsActiveState`
- `internal/daemon/service_test.go::TestPollTrackedDefersTelegramOriginFinalInterruptedAndKeepsActiveState`
- `internal/daemon/service_test.go::TestTelegramOriginHotPollCapturesRunningTool`
- `internal/daemon/service_test.go::TestLiveToolNotificationIgnoresOlderTurnAfterNewerCompletion`
- `internal/daemon/service_test.go::TestRefreshThreadForOperationDefersEmptyInterrupted`

Live E2E:

- checked-in public-safe harness: `tests/live_e2e/telegram_readback_e2e.py`
- requires `CODEX_TG_LIVE_E2E=1`, `CODEX_TG_E2E_THREAD_ID`, a local Telethon session, and bot identity from local env
- uses MTProto readback of edited messages and optional daemon log correlation
- exercises sequential commands plus a multi-command `/reply` math run to catch accidental self-interruption

Contract notes:

- Implicit Telegram-origin `interrupted` is ambiguous until it recovers, expires, or follows explicit `/stop`.
- Deferred terminal state must not collapse the live panel into a false Final Card.
- The daemon must keep polling deferred turns hot.
- Telegram-origin turns get a short App Server `thread/read` hot-poll window
  after start so long-running tools can become visible as an activity even when
  live events do not expose the running command.
- If App Server has not exposed a meaningful tool, the Working card shows normal
  elapsed progress without an empty-tool placeholder.
- Late live tool notifications from older turns must not overwrite a newer
  completed turn or reintroduce stale activities.

## Nil-Safe Telegram Rendering

ADR: `docs/adr/ADR-012-turn-lifecycle-normalization.md`

Primary tests:

- `internal/daemon/log_archive_test.go::TestValueFromMapSkipsNilLikeValues`
- `internal/daemon/log_archive_test.go::TestRenderCommandSkipsNilLikeValues`
- `internal/daemon/log_archive_test.go::TestRenderEventMsgWithoutCommandDoesNotPrintNil`
- `internal/daemon/session_tail_overlay_test.go::TestPollTrackedIgnoresStaleSessionTailTool`
- `internal/daemon/observer_ui_v2_test.go::TestSummaryPanelRemovesNilLiteralBeforeRendering`
- `internal/appserver/client_test.go::TestRPCStringSkipsNilLikeValues`
- `internal/appserver/normalize_test.go::TestStringValueTreatsNilLiteralAsMissing`

Live E2E:

- checked-in public-safe harness: `tests/live_e2e/telegram_readback_e2e.py`
- run against a dedicated private test thread from local env, not the working operator thread
- scenarios: sequential `pwd`, `date`, `printf`, dedicated sleep-20 timing, slow command, and multi-command math through `/reply`
- acceptance: scan edited Working/terminal cards for literal `"<nil>"`, stale
  commands from earlier runs, false parallel-turn rejection, and visible
  non-final `interrupted`

Contract notes:

- Missing App Server fields and literal `"<nil>"` are nil-like values, not display text.
- Telegram rendering must clean nil-like values before Markdown/entity conversion.
- Diagnostics for `telegram_render_contains_nil` are bounded and hash-only.

## Telegram Table And Photo Input Safety

Primary tests:

- `internal/tgformat/markdown_test.go::TestRenderSegmentsConvertsPipeTableToTelegramReadableRecords`
- `internal/tgformat/markdown_test.go::TestRenderSegmentsLeavesPipeTableSyntaxInsideCodeFenceUntouched`
- `internal/tgformat/markdown_test.go::TestMarkdownToHTMLConvertsPipeTableToTelegramReadableRecords`
- `internal/telegram/bot_test.go::TestBotPurePhotoIsDownloadedAndRoutedInsteadOfIgnored`
- `internal/telegram/bot_test.go::TestRemoveStaleTelegramTempFilesRemovesOnlyExpiredPhotoInputs`
- `internal/daemon/service_test.go::TestPendingNewChatConsumesCaptionAndLocalImageAsFirstPrompt`
- `internal/daemon/service_test.go::TestPendingNewChatPhotoWhileWaitingForTitleDoesNotRouteToBinding`

Contract notes:

- Telegram has no native table entity. Preserve information by rendering every
  source row as a mobile-readable record: use its first-column value as the
  title and keep explicit labels for every remaining field.
- Do not rewrite pipe syntax inside fenced code blocks.
- App Server RPC acceptance does not prove that an asynchronous `localImage`
  read has finished. Keep the private input for 30 minutes, schedule deletion,
  and remove matching leftovers older than 24 hours.
- Pending interactive creation takes precedence over the existing binding for a
  photo first prompt. The title stage remains plain-text-only and must fail
  closed rather than misroute the photo.

## Diagnostics And Sanitization

ADR: `docs/adr/ADR-012-turn-lifecycle-normalization.md`

Primary tests:

- `internal/daemon/service_test.go::TestTelegramTurnLifecycleLogsSuccessfulStart`
- `internal/daemon/service_test.go::TestTelegramTurnLifecycleLogsThreadResumeFailure`
- `internal/daemon/service_test.go::TestTelegramTurnLifecycleLogsTurnStartFailure`
- `internal/daemon/service_test.go::TestTelegramTurnLifecycleLogsRefreshFailuresAroundStart`
- `internal/daemon/service_test.go::TestDiagnosticLogsAreRateLimited`
- `internal/daemon/service_test.go::TestDiagnosticLoggerCanBeDisabled`
- `internal/daemon/service_test.go::TestObserverSyncResultLogsAreDebounced`
- `internal/daemon/service_test.go::TestGenericThreadReadDiagnosticsAreDebounced`
- `internal/daemon/service_test.go::TestThreadReadSkippedLogsAreDebounced`
- `internal/telegram/bot_test.go::TestSanitizeTelegramLogErrorRedactsBotTokenURL`
- `tests/config_env_test.go::TestFromEnvDefaultsLoggingOn`
- `tests/config_env_test.go::TestFromEnvInvalidLoggingFlagsFallBackToEnabled`
- `cmd/ctr-go/main_test.go::TestDiagnosticLoggerHonorsFlags`

Contract notes:

- Logs may include ids, route source, operation names, durations, item counts, and sanitized stderr tails.
- Logs must not include full prompt bodies, tokens, session files, SQLite paths, `.env` paths, or unbounded output.
- Diagnostic logging is rate-limited to avoid filesystem floods during app-server loops.
- `CTR_GO_LOG_ENABLED=off` discards daemon stdout logs; `CTR_GO_DIAGNOSTIC_LOGS=off` keeps normal bot logs but suppresses structured lifecycle diagnostics.

## Session Tail Overlay Retirement

ADR: `docs/adr/ADR-013-retire-session-tail-tool-overlay.md`

Feature brief: `docs/process/v0.2.0-live-appserver-events-brief.md`

Primary tests:

- `internal/daemon/session_tail_overlay_test.go::TestPollTrackedIgnoresStaleSessionTailTool`
- `internal/appserver/normalize_test.go::TestToolSnapshotFromLiveNotificationMapsRunningCommand`
- `internal/appserver/normalize_test.go::TestCompactSnapshotStoresToolTimingOnFirstSeen`
- `internal/appserver/normalize_test.go::TestCompactSnapshotPreservesToolTimingWhenUnchanged`
- `internal/appserver/normalize_test.go::TestCompactSnapshotUpdatesToolLastUpdateWhenFingerprintChanges`
- `internal/appserver/normalize_test.go::TestCompactSnapshotDoesNotPreserveActiveLiveToolWhenThreadReadOmitsTool`
- `internal/appserver/normalize_test.go::TestCompactSnapshotDoesNotPreserveLiveToolAcrossTurns`
- `internal/appserver/normalize_test.go::TestSnapshotFromThreadReadKeepsToolOnlyTurnDetailsWithoutCommentary`
- `internal/daemon/service_test.go::TestLiveToolNotificationStoresRunningCommandWithoutRenderingItAsCurrent`
- `internal/daemon/service_test.go::TestPollSnapshotWithoutToolDoesNotPreserveSameTurnRunningToolAsCurrent`
- `internal/daemon/service_test.go::TestPollTrackedDeferredInterruptedDoesNotOverwriteFreshLiveToolSnapshot`
- `internal/daemon/service_test.go::TestPollSnapshotWithOlderCompletedToolPreservesTelegramOriginLiveCurrentTool`
- `internal/daemon/service_test.go::TestRefreshThreadForOperationTerminalCompletedToolReplacesLiveCurrent`
- `internal/daemon/observer_ui_v2_test.go::TestRenderToolPanelShowsLastCompletedToolInsteadOfRunningTool`
- `internal/daemon/observer_ui_v2_test.go::TestRenderToolPanelShowsTelegramOriginCurrentTool`
- `internal/daemon/observer_ui_v2_test.go::TestRenderToolPanelKeepsForeignRunningToolHidden`
- `internal/daemon/observer_ui_v2_test.go::TestRenderSummaryPanelShowsActiveRunElapsedTimeAtBottom`
- `internal/daemon/service_test.go::TestFinalCardShowsRunDuration`

Contract notes:

- App Server `thread/read` snapshots remain the durable source.
- App Server live item notifications may update snapshot/detail history.
- Telegram-origin turns may render current command visibility from live `item/started` and `item/updated` only after matching the marked `thread_id + turn_id`.
- Poll-discovered runs inside the Telegram runtime do not promise authoritative
  current command visibility.
- Long-running active runs render elapsed runtime in the Working card; terminal
  cards render total duration in the header.
- Live tools feed the Activity Aggregator. Fast incidental operations are
  normally count-only; important or long-running operations may become the
  current activity and expose one compact real command.
- While a Telegram-origin live current tool is active, older completed evidence
  may add recent activities but must not replace the current activity.
- Empty/interrupted polling snapshots must not overwrite a fresher stored live current tool for the same Telegram-origin turn.
- Raw output remains available through explicit Details and export surfaces.
- Session JSONL is not a live Telegram UI source.
- Missing App Server tool state does not render placeholder implementation text.
- Session JSONL can still be used for explicit full-log export paths.

Slice gate:

- Each v0.2.0 live-event slice must add or update tests first, pass targeted checks, run the relevant live Telegram E2E case, and only then be committed.

## Baseline Commands

Run before commit or publish:

```powershell
go test ./...
go build -buildvcs=false ./...
git diff --check
git grep -nE "BOT_TOKEN|TELEGRAM_BOT_TOKEN|api_hash|api_id|phone|password|secret|\\.session|\\.sqlite|\\.env|C:\\\\Users\\\\<private-user>" -- ':!go.sum' ':!.env' ':!.env.example' ':!.git'
```
