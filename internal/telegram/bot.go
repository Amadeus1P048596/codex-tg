package telegram

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mideco-tech/codex-tg/internal/config"
	"github.com/mideco-tech/codex-tg/internal/daemon"
	"github.com/mideco-tech/codex-tg/internal/model"
)

const (
	telegramMessageLimit   = 4096
	telegramPhotoMaxSize   = int64(20 << 20)
	telegramPhotoRetention = 30 * time.Minute
	telegramPhotoStaleAge  = 24 * time.Hour
)

var telegramBotTokenURLPattern = regexp.MustCompile(`bot[0-9]+:[A-Za-z0-9_-]+`)

type Bot struct {
	cfg                  config.Config
	client               *Client
	service              *daemon.Service
	logger               *log.Logger
	me                   *User
	schedulePhotoCleanup func([]string, time.Duration)
}

type Document struct {
	Name        string
	ContentType string
	Data        []byte
	Caption     string
}

func NewBot(cfg config.Config, service *daemon.Service, logger *log.Logger) (*Bot, error) {
	if strings.TrimSpace(cfg.TelegramBotToken) == "" {
		return nil, errors.New("CTR_GO_TELEGRAM_BOT_TOKEN is not configured")
	}
	if logger == nil {
		logger = log.Default()
	}
	return &Bot{
		cfg:     cfg,
		client:  NewClient(cfg.TelegramBotToken),
		service: service,
		logger:  logger,
	}, nil
}

func (b *Bot) Start(ctx context.Context) error {
	b.cleanupStaleTelegramPhotoInputs()
	startCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	me, err := b.client.GetMe(startCtx)
	if err != nil {
		return err
	}
	b.me = me
	if err := b.client.SetMyCommands(startCtx, defaultCommands()); err != nil {
		b.logger.Printf("telegram setMyCommands failed: %s", sanitizeTelegramLogError(err))
	}
	b.logger.Printf("telegram bot ready: @%s", me.Username)
	return nil
}

func (b *Bot) cleanupStaleTelegramPhotoInputs() {
	if strings.TrimSpace(b.cfg.Paths.DataDir) == "" {
		return
	}
	directory := filepath.Join(b.cfg.Paths.DataDir, "telegram-inputs")
	if err := removeStaleTelegramTempFiles(directory, time.Now().Add(-telegramPhotoStaleAge)); err != nil && b.logger != nil {
		b.logger.Printf("telegram stale photo cleanup failed: %s", sanitizeTelegramLogError(err))
	}
}

func (b *Bot) Run(ctx context.Context) error {
	var offset int64
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		pollCtx, cancel := context.WithTimeout(ctx, 65*time.Second)
		updates, err := b.client.GetUpdates(pollCtx, offset, 30)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			b.logger.Printf("telegram getUpdates failed: %s", sanitizeTelegramLogError(err))
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(2 * time.Second):
				continue
			}
		}
		for _, update := range updates {
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
			if err := b.handleUpdate(ctx, update); err != nil {
				b.logger.Printf("telegram update %d failed: %s", update.UpdateID, sanitizeTelegramLogError(err))
			}
		}
	}
}

func SanitizeLogError(err error) string {
	if err == nil {
		return ""
	}
	return telegramBotTokenURLPattern.ReplaceAllString(err.Error(), "bot<redacted>")
}

func sanitizeTelegramLogError(err error) string {
	return SanitizeLogError(err)
}

func (b *Bot) SendMessage(ctx context.Context, chatID, topicID int64, text string, buttons [][]model.ButtonSpec, options model.SendOptions) (int64, error) {
	chunks := splitText(strings.TrimSpace(text), telegramMessageLimit)
	if len(chunks) == 0 {
		chunks = []string{" "}
	}
	return b.sendTextChunks(ctx, chatID, topicID, chunks, buttons, options)
}

func (b *Bot) SendRenderedMessages(ctx context.Context, chatID, topicID int64, messages []model.RenderedMessage, buttons [][]model.ButtonSpec, options model.SendOptions) ([]int64, error) {
	if len(messages) == 0 {
		messages = []model.RenderedMessage{{Text: " "}}
	}
	ids := make([]int64, 0, len(messages))
	for index, rendered := range messages {
		if strings.TrimSpace(rendered.Text) == "" {
			rendered.Text = " "
			rendered.Entities = nil
			rendered.ParseMode = ""
			rendered.PlainText = " "
		}
		var markup *InlineKeyboardMarkup
		if index == len(messages)-1 {
			markup = toInlineKeyboard(buttons)
		}
		sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		message, err := b.client.SendRenderedMessage(sendCtx, chatID, topicID, rendered, markup, options)
		cancel()
		if err != nil {
			rendered = plainRenderedFallback(rendered)
			sendCtx, cancel = context.WithTimeout(ctx, 20*time.Second)
			message, err = b.client.SendRenderedMessage(sendCtx, chatID, topicID, rendered, markup, options)
			cancel()
			if err != nil {
				return nil, err
			}
		}
		if message != nil {
			ids = append(ids, message.MessageID)
		}
	}
	return ids, nil
}

func (b *Bot) EditMessage(ctx context.Context, chatID, topicID, messageID int64, text string, buttons [][]model.ButtonSpec) error {
	chunks := splitText(strings.TrimSpace(text), telegramMessageLimit)
	if len(chunks) != 1 {
		return fmt.Errorf("telegram editMessageText requires a single text chunk, got %d", len(chunks))
	}
	editCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	_, err := b.client.EditMessageText(editCtx, chatID, messageID, chunks[0], toInlineKeyboard(buttons))
	cancel()
	return err
}

func (b *Bot) EditRenderedMessage(ctx context.Context, chatID, topicID, messageID int64, rendered model.RenderedMessage, buttons [][]model.ButtonSpec) error {
	if strings.TrimSpace(rendered.Text) == "" {
		rendered.Text = " "
		rendered.Entities = nil
		rendered.ParseMode = ""
		rendered.PlainText = " "
	}
	editCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	_, err := b.client.EditRenderedMessageText(editCtx, chatID, messageID, rendered, toInlineKeyboard(buttons))
	cancel()
	if err == nil {
		return nil
	}
	rendered = plainRenderedFallback(rendered)
	editCtx, cancel = context.WithTimeout(ctx, 20*time.Second)
	_, fallbackErr := b.client.EditRenderedMessageText(editCtx, chatID, messageID, rendered, toInlineKeyboard(buttons))
	cancel()
	if fallbackErr != nil {
		return errors.Join(err, fallbackErr)
	}
	return nil
}

func (b *Bot) SendChatAction(ctx context.Context, chatID, topicID int64, action string) error {
	actionCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return b.client.SendChatAction(actionCtx, chatID, topicID, action)
}

func plainRenderedFallback(rendered model.RenderedMessage) model.RenderedMessage {
	if strings.TrimSpace(rendered.PlainText) != "" {
		rendered.Text = rendered.PlainText
	}
	rendered.Entities = nil
	rendered.ParseMode = ""
	return rendered
}

func (b *Bot) SendDocument(ctx context.Context, chatID, topicID int64, fileName, filePath, caption string, options model.SendOptions) (int64, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return 0, err
	}
	return b.SendDocumentData(ctx, chatID, topicID, fileName, data, caption, options)
}

func (b *Bot) SendDocumentData(ctx context.Context, chatID, topicID int64, fileName string, data []byte, caption string, options model.SendOptions) (int64, error) {
	sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	message, err := b.client.SendDocument(sendCtx, chatID, topicID, DocumentFile{
		Name:        fileName,
		ContentType: "application/octet-stream",
		Data:        data,
	}, strings.TrimSpace(caption), nil, options)
	cancel()
	if err != nil {
		return 0, err
	}
	if message == nil {
		return 0, nil
	}
	return message.MessageID, nil
}

func (b *Bot) DeleteMessage(ctx context.Context, chatID, topicID, messageID int64) error {
	deleteCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return b.client.DeleteMessage(deleteCtx, chatID, messageID)
}

func (b *Bot) handleUpdate(ctx context.Context, update Update) error {
	switch {
	case update.CallbackQuery != nil:
		return b.handleCallback(ctx, *update.CallbackQuery)
	case update.Message != nil:
		return b.handleMessage(ctx, *update.Message)
	case update.EditedMessage != nil:
		return b.handleMessage(ctx, *update.EditedMessage)
	default:
		return nil
	}
}

func (b *Bot) handleMessage(ctx context.Context, message Message) error {
	if message.From == nil {
		return nil
	}
	text := strings.TrimSpace(firstNonEmptyTelegram(message.Text, message.Caption))
	if text == "" && len(message.Photo) == 0 {
		return nil
	}
	replyTo := int64(0)
	if message.ReplyToMessage != nil {
		replyTo = message.ReplyToMessage.MessageID
	}
	localImages, err := b.downloadMessagePhotos(ctx, message.Photo)
	if err != nil {
		return b.sendFailureMessage(ctx, message.Chat.ID, message.MessageThreadID, err)
	}
	response, err := b.service.HandleMessageWithLocalImages(ctx, message.Chat.ID, message.MessageThreadID, message.From.ID, text, localImages, replyTo)
	if err != nil {
		removeTelegramTempFiles(localImages)
		return b.sendFailureMessage(ctx, message.Chat.ID, message.MessageThreadID, err)
	}
	b.retainTelegramTempFiles(localImages)
	return b.deliverDirectResponse(ctx, message.Chat.ID, message.MessageThreadID, response)
}

func (b *Bot) downloadMessagePhotos(ctx context.Context, photos []PhotoSize) ([]string, error) {
	photo, ok := largestTelegramPhoto(photos)
	if !ok {
		return nil, nil
	}
	if photo.FileSize > telegramPhotoMaxSize {
		return nil, fmt.Errorf("Telegram photo exceeds the %d MiB input limit", telegramPhotoMaxSize>>20)
	}
	fileCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	file, err := b.client.GetFile(fileCtx, photo.FileID)
	if err != nil {
		return nil, fmt.Errorf("resolve Telegram photo: %w", err)
	}
	if file == nil || strings.TrimSpace(file.FilePath) == "" {
		return nil, errors.New("Telegram returned no file path for the photo")
	}
	if file.FileSize > telegramPhotoMaxSize {
		return nil, fmt.Errorf("Telegram photo exceeds the %d MiB input limit", telegramPhotoMaxSize>>20)
	}
	data, err := b.client.DownloadFile(fileCtx, file.FilePath, telegramPhotoMaxSize)
	if err != nil {
		return nil, fmt.Errorf("download Telegram photo: %w", err)
	}
	directory := filepath.Join(b.cfg.Paths.DataDir, "telegram-inputs")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	_ = removeStaleTelegramTempFiles(directory, time.Now().Add(-telegramPhotoStaleAge))
	temp, err := os.CreateTemp(directory, "telegram-photo-*.jpg")
	if err != nil {
		return nil, err
	}
	path := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(path)
	}
	if err := temp.Chmod(0o600); err != nil {
		cleanup()
		return nil, err
	}
	if _, err := temp.Write(data); err != nil {
		cleanup()
		return nil, err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return []string{path}, nil
}

func largestTelegramPhoto(photos []PhotoSize) (PhotoSize, bool) {
	var selected PhotoSize
	found := false
	for _, photo := range photos {
		if strings.TrimSpace(photo.FileID) == "" {
			continue
		}
		if !found || int64(photo.Width)*int64(photo.Height) > int64(selected.Width)*int64(selected.Height) ||
			(photo.Width == selected.Width && photo.Height == selected.Height && photo.FileSize > selected.FileSize) {
			selected = photo
			found = true
		}
	}
	return selected, found
}

func removeTelegramTempFiles(paths []string) {
	for _, path := range paths {
		if strings.TrimSpace(path) != "" {
			_ = os.Remove(path)
		}
	}
}

func (b *Bot) retainTelegramTempFiles(paths []string) {
	if len(paths) == 0 {
		return
	}
	retained := append([]string(nil), paths...)
	if b.schedulePhotoCleanup != nil {
		b.schedulePhotoCleanup(retained, telegramPhotoRetention)
		return
	}
	time.AfterFunc(telegramPhotoRetention, func() {
		removeTelegramTempFiles(retained)
	})
}

func removeStaleTelegramTempFiles(directory string, olderThan time.Time) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "telegram-photo-") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.ModTime().Before(olderThan) {
			continue
		}
		if err := os.Remove(filepath.Join(directory, entry.Name())); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func firstNonEmptyTelegram(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (b *Bot) handleCallback(ctx context.Context, callback CallbackQuery) error {
	if callback.From == nil {
		return nil
	}
	chatID := int64(0)
	topicID := int64(0)
	if callback.Message != nil {
		chatID = callback.Message.Chat.ID
		topicID = callback.Message.MessageThreadID
	}
	messageID := int64(0)
	if callback.Message != nil {
		messageID = callback.Message.MessageID
	}
	response, err := b.service.HandleCallback(ctx, chatID, topicID, messageID, callback.From.ID, callback.Data)
	answerText := ""
	if err != nil {
		answerText = "Request failed."
	} else if response == nil {
		answerText = "Ignored."
	} else if strings.TrimSpace(response.CallbackText) != "" {
		answerText = response.CallbackText
	}
	answerCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	_ = b.client.AnswerCallbackQuery(answerCtx, callback.ID, answerText, false)
	cancel()
	if err != nil {
		return b.sendFailureMessage(ctx, chatID, topicID, err)
	}
	return b.deliverDirectResponse(ctx, chatID, topicID, response)
}

func (b *Bot) deliverDirectResponse(ctx context.Context, chatID, topicID int64, response *daemon.DirectResponse) error {
	if response == nil || strings.TrimSpace(response.Text) == "" {
		return nil
	}
	messageID, err := b.SendMessage(ctx, chatID, topicID, response.Text, response.Buttons, model.SendOptions{Silent: true})
	if err != nil {
		return err
	}
	return b.service.RegisterDirectDelivery(ctx, chatID, topicID, messageID, response)
}

func (b *Bot) sendFailureMessage(ctx context.Context, chatID, topicID int64, cause error) error {
	if chatID == 0 {
		if cause != nil {
			b.logger.Printf("telegram handler error without chat context: %v", cause)
		}
		return nil
	}
	text := "Request failed inside the local Go bridge. Try /repair or /status."
	if cause != nil {
		b.logger.Printf("telegram handler error: %v", cause)
	}
	_, err := b.SendMessage(ctx, chatID, topicID, text, nil, model.SendOptions{Silent: true})
	if err != nil {
		if cause != nil {
			return errors.Join(cause, err)
		}
		return err
	}
	return nil
}

func defaultCommands() []BotCommand {
	return []BotCommand{
		{Command: "start", Description: "查看连接状态和快捷帮助"},
		{Command: "home", Description: "打开会话首页"},
		{Command: "help", Description: "查看命令列表"},
		{Command: "status", Description: "查看运行和连接状态"},
		{Command: "current", Description: "确认当前会话"},
		{Command: "inbox", Description: "查看后台待处理会话"},
		{Command: "threads", Description: "切换可用会话"},
		{Command: "projects", Description: "查看项目"},
		{Command: "newchat", Description: "新建 Codex Chat"},
		{Command: "newthread", Description: "新建普通会话"},
		{Command: "cancel", Description: "取消待输入的新建请求"},
		{Command: "title", Description: "修改当前会话标题"},
		{Command: "archive", Description: "归档当前会话"},
		{Command: "unarchive", Description: "恢复已归档会话"},
		{Command: "show", Description: "显示会话卡片"},
		{Command: "bind", Description: "在 TG 独立会话中继续"},
		{Command: "reply", Description: "向会话发送内容"},
		{Command: "plan", Description: "在会话中启动 Plan 模式"},
		{Command: "settings", Description: "查看 Codex 设置"},
		{Command: "model", Description: "选择模型"},
		{Command: "effort", Description: "选择推理强度"},
		{Command: "context", Description: "查看当前路由上下文"},
		{Command: "observe", Description: "开启或关闭观察模式"},
		{Command: "panelmode", Description: "切换卡片生命周期模式"},
		{Command: "release", Description: "释放空闲 live 写入权"},
		{Command: "repair", Description: "重启会话服务"},
		{Command: "stop", Description: "停止当前任务"},
		{Command: "approve", Description: "允许待确认操作"},
		{Command: "deny", Description: "拒绝待确认操作"},
	}
}

func toInlineKeyboard(rows [][]model.ButtonSpec) *InlineKeyboardMarkup {
	if len(rows) == 0 {
		return nil
	}
	keyboard := make([][]InlineKeyboardButton, 0, len(rows))
	for _, row := range rows {
		buttonRow := make([]InlineKeyboardButton, 0, len(row))
		for _, button := range row {
			if strings.TrimSpace(button.Text) == "" {
				continue
			}
			buttonRow = append(buttonRow, InlineKeyboardButton{
				Text:         button.Text,
				CallbackData: button.CallbackData,
			})
		}
		if len(buttonRow) > 0 {
			keyboard = append(keyboard, buttonRow)
		}
	}
	if len(keyboard) == 0 {
		return nil
	}
	return &InlineKeyboardMarkup{InlineKeyboard: keyboard}
}

func (b *Bot) sendTextChunks(ctx context.Context, chatID, topicID int64, chunks []string, buttons [][]model.ButtonSpec, options model.SendOptions) (int64, error) {
	var messageID int64
	for index, chunk := range chunks {
		var markup *InlineKeyboardMarkup
		if index == len(chunks)-1 {
			markup = toInlineKeyboard(buttons)
		}
		sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		message, err := b.client.SendMessage(sendCtx, chatID, topicID, chunk, markup, options)
		cancel()
		if err != nil {
			return 0, err
		}
		if message != nil {
			messageID = message.MessageID
		}
	}
	return messageID, nil
}

func splitText(text string, limit int) []string {
	if limit <= 0 {
		return []string{text}
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if len(text) <= limit {
		return []string{text}
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	current := strings.Builder{}
	flush := func() {
		if current.Len() == 0 {
			return
		}
		out = append(out, strings.TrimSpace(current.String()))
		current.Reset()
	}
	for _, line := range lines {
		line = strings.TrimRight(line, " ")
		candidate := line
		if current.Len() > 0 {
			candidate = current.String() + "\n" + line
		}
		if len(candidate) <= limit {
			if current.Len() > 0 {
				current.WriteByte('\n')
			}
			current.WriteString(line)
			continue
		}
		flush()
		for len(line) > limit {
			out = append(out, strings.TrimSpace(line[:limit]))
			line = line[limit:]
		}
		if line != "" {
			current.WriteString(line)
		}
	}
	flush()
	if len(out) == 0 {
		return []string{text}
	}
	return out
}

func (b *Bot) String() string {
	if b.me == nil {
		return "telegram bot"
	}
	return fmt.Sprintf("@%s", b.me.Username)
}
