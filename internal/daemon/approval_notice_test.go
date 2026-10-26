package daemon

import (
	"context"
	"github.com/mideco-tech/codex-tg/internal/appserver"
	"github.com/mideco-tech/codex-tg/internal/model"
	"strings"
	"testing"
)

func TestApprovalNoticeIsSeparateNotifyingAndDeduplicated(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	sender := &recordingSender{}
	s.SetSender(sender)
	a := model.PendingApproval{RequestID: "runtime:1:0", ThreadID: "approval-thread", TurnID: "approval-turn", PromptKind: "approval", Question: "Allow?", Status: "pending", PayloadJSON: `{"reason":"Write outside workspace","command":"touch example"}`, UpdatedAt: model.NowString()}
	if err := s.store.SavePendingApproval(ctx, a); err != nil {
		t.Fatal(err)
	}
	s.enqueueApprovalNotice(ctx, a)
	s.enqueueApprovalNotice(ctx, a)
	s.processDeliveryBatch(ctx)
	if len(sender.messages) != 1 {
		t.Fatalf("messages=%d, want one independent approval notice", len(sender.messages))
	}
	m := sender.messages[0]
	if m.options.Silent || !strings.Contains(m.text, "Write outside workspace") {
		t.Fatalf("missing notifying approval reason: %#v", m)
	}
	token := callbackTokenForButton(m.buttons, "允许")
	if token == "" {
		t.Fatal("missing allow button")
	}
	route, err := s.store.GetCallbackRoute(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if route.ThreadID != a.ThreadID || route.TurnID != a.TurnID || route.RequestID != a.RequestID {
		t.Fatalf("incorrect callback routing: %#v", route)
	}
	s.processDeliveryBatch(ctx)
	if len(sender.messages) != 1 {
		t.Fatal("duplicate notice")
	}
}

func TestResolvedApprovalNoticeIsNotDelivered(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	sender := &recordingSender{}
	s.SetSender(sender)
	a := model.PendingApproval{RequestID: "runtime:1:0", ThreadID: "approval-thread", PromptKind: "approval", Status: "pending", PayloadJSON: "{}", UpdatedAt: model.NowString()}
	if err := s.store.SavePendingApproval(ctx, a); err != nil {
		t.Fatal(err)
	}
	s.enqueueApprovalNotice(ctx, a)
	if err := s.store.UpdatePendingApprovalStatus(ctx, a.RequestID, "resolved"); err != nil {
		t.Fatal(err)
	}
	s.processDeliveryBatch(ctx)
	if len(sender.messages) != 0 {
		t.Fatal("sent stale approval")
	}
}

func TestApprovalEventQueuesIndependentNotice(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	sender := &recordingSender{}
	s.SetSender(sender)
	live := &stubSession{}
	event := appserver.Event{Channel: "server_request", Method: "item/commandExecution/requestApproval", ID: "runtime:1:0", Params: map[string]any{"threadId": "new-thread", "turnId": "new-turn", "itemId": "new-item", "reason": "Permission required"}}
	s.handleLiveEvent(ctx, live, event)
	s.processDeliveryBatch(ctx)
	found := false
	for _, m := range sender.messages {
		if strings.Contains(m.text, "Permission required") && !m.options.Silent && callbackTokenForButton(m.buttons, "允许") != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("approval event did not send independent notification: %#v", sender.messages)
	}
}

func TestApprovalButtonsRespectAvailableDecisions(t *testing.T) {
	s := newTestService(t)
	a := model.PendingApproval{RequestID: "runtime:1:0", ThreadID: "thread", PayloadJSON: `{"availableDecisions":["accept","cancel"]}`}
	response := s.renderPendingApproval(context.Background(), a)
	if callbackTokenForButton(response.Buttons, "允许") == "" || callbackTokenForButton(response.Buttons, "取消") == "" {
		t.Fatal("missing supported decision")
	}
	if callbackTokenForButton(response.Buttons, "本次会话允许") != "" || callbackTokenForButton(response.Buttons, "拒绝") != "" {
		t.Fatal("unsupported approval decision exposed")
	}
}

func TestApprovalBeforeActivityCardUsesBoundTopic(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	sender := &recordingSender{}
	s.SetSender(sender)
	s.cfg.AllowedUserIDs = []int64{111, 222}
	if err := s.store.SetGlobalObserverTarget(ctx, 111, 0, false); err != nil {
		t.Fatal(err)
	}
	if err := s.store.SetBinding(ctx, -100123, 7, "fast-thread", "write"); err != nil {
		t.Fatal(err)
	}
	a := model.PendingApproval{RequestID: "runtime:1:2", ThreadID: "fast-thread", PromptKind: "approval", Status: "pending", PayloadJSON: "{}", UpdatedAt: model.NowString()}
	if err := s.store.SavePendingApproval(ctx, a); err != nil {
		t.Fatal(err)
	}
	s.enqueueApprovalNotice(ctx, a)
	s.processDeliveryBatch(ctx)
	if len(sender.messages) != 1 || sender.messages[0].chatID != -100123 || sender.messages[0].topicID != 7 {
		t.Fatalf("approval did not reach originating topic: %#v", sender.messages)
	}
}
