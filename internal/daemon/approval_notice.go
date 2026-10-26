package daemon

import (
	"context"

	"github.com/mideco-tech/codex-tg/internal/model"
	"github.com/mideco-tech/codex-tg/internal/storage"
)

// Keep approvals audible even when their thread is in the background.
func (s *Service) enqueueApprovalNotice(ctx context.Context, approval model.PendingApproval) {
	targets := make(map[string]model.ObserverTarget)
	bindings, err := s.store.ListBindingTargetsForThread(ctx, approval.ThreadID)
	if err != nil {
		s.setError(ctx, err)
		return
	}
	for _, target := range bindings {
		targets[target.ChatKey] = target
	}
	panels, err := s.store.ListCurrentPanelsForThread(ctx, approval.ThreadID)
	if err != nil {
		s.setError(ctx, err)
		return
	}
	for _, panel := range panels {
		key := model.ChatKey(panel.ChatID, panel.TopicID)
		targets[key] = model.ObserverTarget{ChatKey: key, ChatID: panel.ChatID, TopicID: panel.TopicID, Enabled: true}
	}
	if len(targets) == 0 {
		background, _ := s.backgroundTargets(ctx)
		if len(background) == 0 {
			background = s.defaultBackgroundTargets()
		}
		for _, target := range background {
			targets[model.ChatKey(target.ChatID, target.TopicID)] = target
		}
	}
	for key, target := range targets {
		response := s.renderPendingApproval(ctx, approval)
		payload := model.DeliveryPayload{Text: response.Text, ThreadID: approval.ThreadID, TurnID: approval.TurnID, ItemID: approval.ItemID, EventID: approval.RequestID, Buttons: response.Buttons}
		err := s.store.EnqueueDelivery(ctx, model.DeliveryQueueItem{EventID: "approval:" + approval.RequestID, ChatKey: key, ChatID: target.ChatID, TopicID: target.TopicID, ThreadID: approval.ThreadID, Kind: "approval", Status: model.DeliveryStatusPending, PayloadJSON: storage.MustJSON(payload)})
		if err != nil {
			s.setError(ctx, err)
		}
	}
}
