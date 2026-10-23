package daemon

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/mideco-tech/codex-tg/internal/model"
	"github.com/mideco-tech/codex-tg/internal/tgformat"
)

const (
	pushHTTPMaxBodyBytes     = 64 << 10
	pushSourceMaxRunes       = 64
	pushTitleMaxRunes        = 120
	pushConversationMaxRunes = 160
	pushMessageBodyMaxRunes  = 3500
	pushIdempotencyMaxRunes  = 200
	pushShutdownTimeout      = 5 * time.Second
	pushDeliveryTimeout      = 25 * time.Second
	pushDeliveredStatePrefix = "push.delivered."
)

type PushRequest struct {
	Source            string `json:"source"`
	Title             string `json:"title"`
	Body              string `json:"body"`
	ConversationTitle string `json:"conversation_title,omitempty"`
	Severity          string `json:"severity,omitempty"`
	IdempotencyKey    string `json:"idempotency_key"`
}

type pushResult struct {
	Duplicate bool
}

func validatePushConfig(listen, token string) error {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		return nil
	}
	if len(strings.TrimSpace(token)) < 32 {
		return errors.New("CTR_GO_PUSH_TOKEN must contain at least 32 characters when push HTTP is enabled")
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil || strings.TrimSpace(port) == "" {
		return errors.New("CTR_GO_PUSH_LISTEN must be an explicit host:port address")
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" || host == "*" || host == "0.0.0.0" || host == "::" {
		return errors.New("CTR_GO_PUSH_LISTEN must bind an explicit loopback, LAN, or private-network address")
	}
	return nil
}

func validatePushRequest(request PushRequest) (PushRequest, error) {
	request.Source = strings.TrimSpace(request.Source)
	request.Title = strings.TrimSpace(request.Title)
	request.Body = strings.TrimSpace(strings.ReplaceAll(request.Body, "\r\n", "\n"))
	request.ConversationTitle = strings.TrimSpace(request.ConversationTitle)
	request.Severity = strings.ToLower(strings.TrimSpace(request.Severity))
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if request.Severity == "" {
		request.Severity = "normal"
	}
	for _, field := range []struct {
		name     string
		value    string
		maxRunes int
		required bool
		oneLine  bool
	}{
		{name: "source", value: request.Source, maxRunes: pushSourceMaxRunes, required: true, oneLine: true},
		{name: "title", value: request.Title, maxRunes: pushTitleMaxRunes, required: true, oneLine: true},
		{name: "body", value: request.Body, maxRunes: pushMessageBodyMaxRunes, required: true},
		{name: "conversation_title", value: request.ConversationTitle, maxRunes: pushConversationMaxRunes, oneLine: true},
		{name: "idempotency_key", value: request.IdempotencyKey, maxRunes: pushIdempotencyMaxRunes, required: true, oneLine: true},
	} {
		if field.required && field.value == "" {
			return PushRequest{}, fmt.Errorf("%s is required", field.name)
		}
		if len([]rune(field.value)) > field.maxRunes {
			return PushRequest{}, fmt.Errorf("%s exceeds %d characters", field.name, field.maxRunes)
		}
		if field.oneLine && strings.ContainsAny(field.value, "\r\n") {
			return PushRequest{}, fmt.Errorf("%s must be a single line", field.name)
		}
	}
	if request.Severity != "normal" && request.Severity != "silent" {
		return PushRequest{}, errors.New("severity must be normal or silent")
	}
	return request, nil
}

func (s *Service) publishPush(ctx context.Context, request PushRequest) (pushResult, error) {
	request, err := validatePushRequest(request)
	if err != nil {
		return pushResult{}, err
	}
	target, err := s.currentBackgroundTarget(ctx)
	if err != nil {
		return pushResult{}, fmt.Errorf("resolve Telegram push target: %w", err)
	}
	if target == nil || !target.Enabled {
		return pushResult{}, errors.New("no Telegram push target is configured")
	}
	s.mu.RLock()
	sender := s.sender
	s.mu.RUnlock()
	if sender == nil {
		return pushResult{}, errors.New("Telegram sender is not ready")
	}

	s.pushMu.Lock()
	defer s.pushMu.Unlock()
	dedupeKey := pushDedupeStateKey(*target, request.IdempotencyKey)
	delivered, stateErr := s.store.GetState(ctx, dedupeKey)
	if stateErr != nil {
		return pushResult{}, fmt.Errorf("read Telegram push receipt: %w", stateErr)
	}
	if strings.TrimSpace(delivered) != "" {
		return pushResult{Duplicate: true}, nil
	}
	attempt := map[string]any{"attempted_at": s.automationNow().UTC().Format(time.RFC3339Nano)}
	attemptData, _ := json.Marshal(attempt)
	if err := s.store.SetState(ctx, dedupeKey, string(attemptData)); err != nil {
		return pushResult{}, fmt.Errorf("persist Telegram push attempt: %w", err)
	}
	rendered := renderPushMessages(request)
	options := notifySendOptions()
	if request.Severity == "silent" {
		options = silentSendOptions()
	}
	deliveryCtx, cancelDelivery := context.WithTimeout(context.WithoutCancel(ctx), pushDeliveryTimeout)
	defer cancelDelivery()
	messageIDs, err := sender.SendRenderedMessages(deliveryCtx, target.ChatID, target.TopicID, rendered, nil, options)
	if err != nil {
		return pushResult{}, fmt.Errorf("send Telegram push: %w", err)
	}
	receipt := map[string]any{"delivered_at": s.automationNow().UTC().Format(time.RFC3339Nano)}
	if len(messageIDs) > 0 {
		receipt["message_id"] = messageIDs[0]
	}
	data, _ := json.Marshal(receipt)
	if err := s.store.SetState(ctx, dedupeKey, string(data)); err != nil {
		return pushResult{}, fmt.Errorf("persist Telegram push receipt: %w", err)
	}
	return pushResult{}, nil
}

func pushDedupeStateKey(target model.ObserverTarget, idempotencyKey string) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%d\x00%s", target.ChatID, target.TopicID, strings.TrimSpace(idempotencyKey))))
	return pushDeliveredStatePrefix + hex.EncodeToString(digest[:])
}

func renderPushMessages(request PushRequest) []model.RenderedMessage {
	header := "🔔 " + request.Title + "\n来源：" + request.Source
	if request.ConversationTitle != "" {
		header += "\n归属会话：" + request.ConversationTitle
	}
	return tgformat.RenderSegments([]tgformat.Segment{
		tgformat.Plain(header + "\n\n"),
		tgformat.Markdown(request.Body),
	}, outputMessageLimit)
}

func (s *Service) pushHTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/push", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			writer.Header().Set("Allow", http.MethodPost)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !validPushBearer(request.Header.Get("Authorization"), s.cfg.PushToken) {
			writer.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		if contentType := strings.TrimSpace(request.Header.Get("Content-Type")); contentType != "" {
			mediaType, _, err := mime.ParseMediaType(contentType)
			if err != nil || !strings.EqualFold(mediaType, "application/json") {
				http.Error(writer, "content type must be application/json", http.StatusUnsupportedMediaType)
				return
			}
		}
		if request.ContentLength > pushHTTPMaxBodyBytes {
			http.Error(writer, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		request.Body = http.MaxBytesReader(writer, request.Body, pushHTTPMaxBodyBytes)
		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()
		var push PushRequest
		if err := decoder.Decode(&push); err != nil {
			var maxBytesError *http.MaxBytesError
			if errors.As(err, &maxBytesError) {
				http.Error(writer, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(writer, "invalid JSON request", http.StatusBadRequest)
			return
		}
		if err := ensureJSONEOF(decoder); err != nil {
			http.Error(writer, "invalid JSON request", http.StatusBadRequest)
			return
		}
		result, err := s.publishPush(request.Context(), push)
		if err != nil {
			status := http.StatusBadRequest
			if strings.Contains(err.Error(), "target") || strings.Contains(err.Error(), "sender") {
				status = http.StatusServiceUnavailable
			} else if strings.Contains(err.Error(), "send Telegram") || strings.Contains(err.Error(), "persist Telegram") {
				status = http.StatusBadGateway
			}
			http.Error(writer, http.StatusText(status), status)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(writer).Encode(map[string]any{"ok": true, "duplicate": result.Duplicate})
	})
	return mux
}

func validPushBearer(header, expected string) bool {
	provided, ok := strings.CutPrefix(strings.TrimSpace(header), "Bearer ")
	if !ok || len(provided) != len(expected) || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values")
	}
	return err
}

func (s *Service) startPushHTTP(ctx context.Context) error {
	listenAddress := strings.TrimSpace(s.cfg.PushListen)
	if listenAddress == "" {
		return nil
	}
	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		return fmt.Errorf("listen for codex-tg pushes: %w", err)
	}
	server := &http.Server{
		Handler:           s.pushHTTPHandler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	s.spawn(ctx, func(ctx context.Context) {
		shutdownDone := make(chan struct{})
		go func() {
			defer close(shutdownDone)
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), pushShutdownTimeout)
			defer cancel()
			_ = server.Shutdown(shutdownCtx)
		}()
		serveErr := server.Serve(listener)
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) && ctx.Err() == nil {
			s.setError(ctx, fmt.Errorf("codex-tg push server stopped: %w", serveErr))
		}
		if ctx.Err() != nil {
			<-shutdownDone
		}
	})
	return nil
}
