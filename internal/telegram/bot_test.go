package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mideco-tech/codex-tg/internal/config"
	"github.com/mideco-tech/codex-tg/internal/daemon"
	"github.com/mideco-tech/codex-tg/internal/model"
)

func TestBotEditMessageRejectsMultiChunkPayload(t *testing.T) {
	t.Parallel()

	bot := &Bot{client: NewClient("token")}
	err := bot.EditMessage(context.Background(), 42, 0, 77, strings.Repeat("x", telegramMessageLimit+10), nil)
	if err == nil {
		t.Fatal("EditMessage must reject multi-chunk payloads")
	}
}

func TestSanitizeTelegramLogErrorRedactsBotTokenURL(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf(`Post "https://api.telegram.org/bot123456789:AAF_secret-token/getUpdates": context deadline exceeded`)
	got := sanitizeTelegramLogError(err)
	if strings.Contains(got, "123456789:AAF_secret-token") {
		t.Fatalf("sanitizeTelegramLogError leaked token: %q", got)
	}
	if !strings.Contains(got, "bot<redacted>") {
		t.Fatalf("sanitizeTelegramLogError = %q, want redacted marker", got)
	}
}

func TestDefaultCommandsExposeNewChatMenuCommand(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool)
	descriptions := make(map[string]string)
	for _, command := range defaultCommands() {
		if seen[command.Command] {
			t.Fatalf("defaultCommands contains duplicate command %q", command.Command)
		}
		seen[command.Command] = true
		descriptions[command.Command] = command.Description
	}
	for _, command := range []string{"home", "current", "inbox", "newchat", "newthread", "cancel", "title", "archive", "unarchive", "release"} {
		if !seen[command] {
			t.Fatalf("defaultCommands must expose /%s in the Telegram command menu", command)
		}
	}
	if seen["default"] {
		t.Fatal("defaultCommands must not expose hidden /default fallback in the Telegram command menu")
	}
	if got := descriptions["bind"]; got != "在 TG 独立会话中继续" {
		t.Fatalf("/bind description = %q, want isolated-runtime wording", got)
	}
	if got := descriptions["release"]; got != "释放空闲 live 写入权" {
		t.Fatalf("/release description = %q, want live-session wording", got)
	}
}

func TestBotSendMessageChunksAndReturnsLastMessageID(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = fmt.Fprintf(w, `{"ok":true,"result":{"message_id":%d,"chat":{"id":42,"type":"private"}}}`, 100+calls)
	}))
	defer server.Close()

	client := NewClient("token")
	client.baseURL = server.URL
	bot := &Bot{client: client}

	messageID, err := bot.SendMessage(context.Background(), 42, 0, strings.Repeat("line\n", telegramMessageLimit/4), nil, model.SendOptions{})
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	if calls < 2 {
		t.Fatalf("calls = %d, want at least 2 chunked requests", calls)
	}
	if got, want := messageID, int64(100+calls); got != want {
		t.Fatalf("messageID = %d, want %d", got, want)
	}
}

func TestBotSendRenderedMessagesFallsBackToPlainEntities(t *testing.T) {
	t.Parallel()

	var calls int
	var second map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll failed: %v", err)
		}
		if calls == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: entities are invalid"}`))
			return
		}
		if err := json.Unmarshal(body, &second); err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":202,"chat":{"id":42,"type":"private"}}}`))
	}))
	defer server.Close()

	client := NewClient("token")
	client.baseURL = server.URL
	bot := &Bot{client: client}
	ids, err := bot.SendRenderedMessages(context.Background(), 42, 0, []model.RenderedMessage{{
		Text:     "formatted",
		Entities: []model.MessageEntity{{Type: "code", Offset: 0, Length: 9}},
	}}, nil, model.SendOptions{})
	if err != nil {
		t.Fatalf("SendRenderedMessages failed: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if len(ids) != 1 || ids[0] != 202 {
		t.Fatalf("ids = %#v, want [202]", ids)
	}
	if _, ok := second["entities"]; ok {
		t.Fatalf("fallback entities = %#v, want omitted", second["entities"])
	}
	if _, ok := second["parse_mode"]; ok {
		t.Fatalf("fallback parse_mode = %#v, want omitted", second["parse_mode"])
	}
}

func TestBotSendDocumentReturnsTelegramMessageID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":555,"chat":{"id":42,"type":"private"}}}`))
	}))
	defer server.Close()

	client := NewClient("token")
	client.baseURL = server.URL
	bot := &Bot{client: client}

	dir := t.TempDir()
	path := filepath.Join(dir, "trace.log")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile(trace.log) failed: %v", err)
	}

	messageID, err := bot.SendDocument(context.Background(), 42, 0, "trace.log", path, "trace", model.SendOptions{})
	if err != nil {
		t.Fatalf("SendDocument failed: %v", err)
	}
	if got, want := messageID, int64(555); got != want {
		t.Fatalf("messageID = %d, want %d", got, want)
	}
}

func TestLargestTelegramPhotoSelectsHighestResolution(t *testing.T) {
	t.Parallel()

	photo, ok := largestTelegramPhoto([]PhotoSize{
		{FileID: "small", Width: 90, Height: 90, FileSize: 100},
		{FileID: "large", Width: 1280, Height: 720, FileSize: 1000},
		{FileID: "medium", Width: 640, Height: 480, FileSize: 500},
	})
	if !ok || photo.FileID != "large" {
		t.Fatalf("photo=%#v ok=%v, want large", photo, ok)
	}
}

func TestBotPurePhotoIsDownloadedAndRoutedInsteadOfIgnored(t *testing.T) {
	t.Parallel()

	var getFileCalls int
	var downloadCalls int
	var sendCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/getFile":
			getFileCalls++
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_id":"photo-large","file_size":8,"file_path":"photos/input.jpg"}}`))
		case "/photos/input.jpg":
			downloadCalls++
			_, _ = w.Write([]byte("jpegdata"))
		case "/sendMessage":
			sendCalls++
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":778,"chat":{"id":42,"type":"private"}}}`))
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer server.Close()

	root := t.TempDir()
	cfg := config.Config{
		Paths: config.Paths{
			Home: root, DataDir: filepath.Join(root, "data"), LogDir: filepath.Join(root, "logs"), DBPath: filepath.Join(root, "data", "state.sqlite"),
		},
		AllowedUserIDs: []int64{7},
	}
	service, err := daemon.New(cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })

	client := NewClient("token")
	client.baseURL = server.URL
	client.fileBaseURL = server.URL
	bot := &Bot{cfg: cfg, client: client, service: service}
	err = bot.handleMessage(context.Background(), Message{
		MessageID: 1,
		From:      &User{ID: 7},
		Chat:      Chat{ID: 42, Type: "private"},
		Photo: []PhotoSize{
			{FileID: "photo-small", Width: 100, Height: 100},
			{FileID: "photo-large", Width: 1000, Height: 800, FileSize: 8},
		},
	})
	if err != nil {
		t.Fatalf("handleMessage failed: %v", err)
	}
	if getFileCalls != 1 || downloadCalls != 1 || sendCalls != 1 {
		t.Fatalf("calls getFile=%d download=%d send=%d, want 1/1/1", getFileCalls, downloadCalls, sendCalls)
	}
	entries, err := os.ReadDir(filepath.Join(root, "data", "telegram-inputs"))
	if err != nil {
		t.Fatalf("ReadDir telegram-inputs failed: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary photo inputs were not removed: %#v", entries)
	}
}

func TestBotDeliverDirectResponseSendsSilentMessage(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll failed: %v", err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":777,"chat":{"id":42,"type":"private"},"text":"menu"}}`))
	}))
	defer server.Close()

	root := t.TempDir()
	service, err := daemon.New(config.Config{Paths: config.Paths{
		Home:    root,
		DataDir: filepath.Join(root, "data"),
		LogDir:  filepath.Join(root, "logs"),
		DBPath:  filepath.Join(root, "data", "state.sqlite"),
	}})
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })

	client := NewClient("token")
	client.baseURL = server.URL
	bot := &Bot{client: client, service: service}
	if err := bot.deliverDirectResponse(context.Background(), 42, 0, &daemon.DirectResponse{Text: "menu"}); err != nil {
		t.Fatalf("deliverDirectResponse failed: %v", err)
	}
	if got, ok := captured["disable_notification"].(bool); !ok || !got {
		t.Fatalf("disable_notification = %#v, want true", captured["disable_notification"])
	}
}
