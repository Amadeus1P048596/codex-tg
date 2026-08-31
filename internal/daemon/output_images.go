package daemon

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/mideco-tech/codex-tg/internal/appserver"
	"github.com/mideco-tech/codex-tg/internal/model"
)

const (
	maxTelegramOutputImageBytes    = 10 << 20
	maxTelegramOutputImagesPerTurn = 4
)

var markdownImagePattern = regexp.MustCompile(`!\[([^\]]*)\]\(([^\r\n)]+)\)`)

type outputImageCandidate struct {
	key         string
	fingerprint string
	fileName    string
	caption     string
	data        []byte
}

func (s *Service) maybeSendFinalImages(ctx context.Context, sender Sender, target model.ObserverTarget, thread model.Thread, snapshot *appserver.ThreadReadSnapshot) error {
	if sender == nil || snapshot == nil || !isTerminalStatus(snapshot.LatestTurnStatus) {
		return nil
	}
	candidates, err := collectFinalImageCandidates(thread, snapshot)
	if err != nil && len(candidates) == 0 {
		return err
	}
	var sendErrors []error
	if err != nil {
		sendErrors = append(sendErrors, err)
	}
	for _, candidate := range candidates {
		stateKey := finalImageDeliveryStateKey(target, thread.ID, snapshot.LatestTurnID, candidate.fingerprint)
		previous, stateErr := s.store.GetState(ctx, stateKey)
		if stateErr != nil {
			sendErrors = append(sendErrors, fmt.Errorf("read Telegram image delivery state: %w", stateErr))
			continue
		}
		if strings.TrimSpace(previous) == "sent" {
			continue
		}
		messageID, sendErr := sender.SendPhotoData(ctx, target.ChatID, target.TopicID, candidate.fileName, candidate.data, candidate.caption, silentSendOptions())
		if sendErr != nil {
			sendErrors = append(sendErrors, fmt.Errorf("send Codex image to Telegram: %w", sendErr))
			continue
		}
		if setErr := s.store.SetState(ctx, stateKey, "sent"); setErr != nil {
			sendErrors = append(sendErrors, fmt.Errorf("persist Telegram image delivery state: %w", setErr))
			continue
		}
		if messageID != 0 {
			_ = s.store.PutMessageRoute(ctx, model.MessageRoute{
				ChatID: target.ChatID, TopicID: target.TopicID, MessageID: messageID,
				ThreadID: thread.ID, TurnID: snapshot.LatestTurnID, EventID: "final_image:" + candidate.fingerprint,
				CreatedAt: model.NowString(),
			})
		}
	}
	return errors.Join(sendErrors...)
}

func collectFinalImageCandidates(thread model.Thread, snapshot *appserver.ThreadReadSnapshot) ([]outputImageCandidate, error) {
	if snapshot == nil {
		return nil, nil
	}
	candidates := make([]outputImageCandidate, 0, len(snapshot.LatestOutputImages))
	seen := map[string]struct{}{}
	var candidateErrors []error
	for _, image := range snapshot.LatestOutputImages {
		candidate, err := structuredOutputImageCandidate(image)
		if err != nil {
			candidateErrors = append(candidateErrors, err)
			continue
		}
		if _, ok := seen[candidate.key]; ok {
			continue
		}
		seen[candidate.key] = struct{}{}
		candidates = append(candidates, candidate)
		if len(candidates) >= maxTelegramOutputImagesPerTurn {
			return candidates, errors.Join(candidateErrors...)
		}
	}
	var markdownMatches [][]string
	_ = mapMarkdownOutsideFences(snapshot.LatestFinalText, func(line string) string {
		markdownMatches = append(markdownMatches, markdownImagePattern.FindAllStringSubmatch(line, -1)...)
		return line
	})
	for _, match := range markdownMatches {
		if len(match) < 3 {
			continue
		}
		candidate, err := markdownOutputImageCandidate(thread.CWD, match[2], match[1], snapshot.LatestFinalFP)
		if err != nil {
			// Markdown can legitimately contain remote images or examples. Ignore
			// anything that is not an existing, safe local image in this thread.
			continue
		}
		if _, ok := seen[candidate.key]; ok {
			continue
		}
		seen[candidate.key] = struct{}{}
		candidates = append(candidates, candidate)
		if len(candidates) >= maxTelegramOutputImagesPerTurn {
			break
		}
	}
	return candidates, errors.Join(candidateErrors...)
}

func structuredOutputImageCandidate(image appserver.OutputImage) (outputImageCandidate, error) {
	path := strings.TrimSpace(image.Path)
	if path != "" {
		if !filepath.IsAbs(path) {
			return outputImageCandidate{}, fmt.Errorf("App Server image path is not absolute")
		}
		cleanPath, err := canonicalExistingFile(path)
		if err != nil {
			return outputImageCandidate{}, fmt.Errorf("read App Server image: %w", err)
		}
		data, err := readOutputImage(cleanPath)
		if err != nil {
			return outputImageCandidate{}, err
		}
		return newOutputImageCandidate("path:"+pathComparisonKey(cleanPath), image.Fingerprint, filepath.Base(cleanPath), image.Caption, data)
	}
	data, err := decodeImageResult(image.Result)
	if err != nil {
		return outputImageCandidate{}, err
	}
	fingerprint := strings.TrimSpace(image.Fingerprint)
	if fingerprint == "" {
		fingerprint = hashStrings("image-result", image.ID, image.Result)
	}
	return newOutputImageCandidate("result:"+fingerprint, fingerprint, "codex-image-"+visualShortID(image.ID)+".png", image.Caption, data)
}

func markdownOutputImageCandidate(cwd, rawTarget, alt, finalFP string) (outputImageCandidate, error) {
	path, err := resolveMarkdownImagePath(cwd, rawTarget)
	if err != nil {
		return outputImageCandidate{}, err
	}
	data, err := readOutputImage(path)
	if err != nil {
		return outputImageCandidate{}, err
	}
	key := "path:" + pathComparisonKey(path)
	return newOutputImageCandidate(key, hashStrings("markdown-image", finalFP, key), filepath.Base(path), alt, data)
}

func newOutputImageCandidate(key, fingerprint, fileName, caption string, data []byte) (outputImageCandidate, error) {
	contentType := http.DetectContentType(data)
	if contentType != "image/png" && contentType != "image/jpeg" {
		return outputImageCandidate{}, fmt.Errorf("unsupported Telegram photo content type %q", contentType)
	}
	if strings.TrimSpace(fingerprint) == "" {
		fingerprint = hashStrings(key, string(data))
	}
	fileName = strings.TrimSpace(filepath.Base(fileName))
	if fileName == "" || fileName == "." {
		fileName = "codex-image.png"
	}
	caption = strings.TrimSpace(caption)
	if caption == "" {
		caption = "Codex 图片"
	}
	caption = truncateRunes(caption, 900)
	return outputImageCandidate{key: key, fingerprint: fingerprint, fileName: fileName, caption: caption, data: data}, nil
}

func decodeImageResult(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("App Server image has neither savedPath nor result")
	}
	if strings.HasPrefix(strings.ToLower(raw), "data:") {
		comma := strings.IndexByte(raw, ',')
		if comma < 0 || !strings.Contains(strings.ToLower(raw[:comma]), ";base64") {
			return nil, errors.New("unsupported App Server image data URL")
		}
		raw = raw[comma+1:]
	}
	if len(raw) > base64.StdEncoding.EncodedLen(maxTelegramOutputImageBytes)+4 {
		return nil, fmt.Errorf("App Server image exceeds %d-byte Telegram bridge limit", maxTelegramOutputImageBytes)
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("App Server image result is not base64 data")
	}
	if len(data) == 0 || len(data) > maxTelegramOutputImageBytes {
		return nil, fmt.Errorf("App Server image exceeds %d-byte Telegram bridge limit", maxTelegramOutputImageBytes)
	}
	return data, nil
}

func resolveMarkdownImagePath(cwd, rawTarget string) (string, error) {
	target := strings.TrimSpace(rawTarget)
	if strings.HasPrefix(target, "<") && strings.HasSuffix(target, ">") {
		target = strings.TrimSpace(target[1 : len(target)-1])
	}
	if parsed, err := url.Parse(target); err == nil && strings.EqualFold(parsed.Scheme, "file") {
		target = parsed.Path
		if parsed.Host != "" {
			target = "//" + parsed.Host + target
		}
		if runtime.GOOS == "windows" && len(target) >= 3 && target[0] == '/' && target[2] == ':' {
			target = target[1:]
		}
		if decoded, decodeErr := url.PathUnescape(target); decodeErr == nil {
			target = decoded
		}
	} else if strings.Contains(target, "://") {
		return "", errors.New("remote Markdown image is not a local Telegram attachment")
	}
	root := strings.TrimSpace(cwd)
	if root == "" {
		return "", errors.New("thread working directory is empty")
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, filepath.FromSlash(target))
	}
	cleanPath, err := canonicalExistingFile(target)
	if err != nil {
		return "", err
	}
	cleanRoot, err := canonicalExistingDirectory(root)
	if err != nil {
		return "", err
	}
	if !pathWithinRoot(cleanPath, cleanRoot) {
		return "", errors.New("Markdown image is outside the thread working directory")
	}
	return cleanPath, nil
}

func canonicalExistingFile(path string) (string, error) {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("image path is not a regular file")
	}
	return resolved, nil
}

func canonicalExistingDirectory(path string) (string, error) {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

func pathWithinRoot(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func pathComparisonKey(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

func readOutputImage(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() <= 0 || info.Size() > maxTelegramOutputImageBytes {
		return nil, fmt.Errorf("image size %d is outside the Telegram bridge limit", info.Size())
	}
	return os.ReadFile(path)
}

func finalImageDeliveryStateKey(target model.ObserverTarget, threadID, turnID, fingerprint string) string {
	return "ui.final_image." + hashStrings(model.ChatKey(target.ChatID, target.TopicID), threadID, turnID, fingerprint)
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func telegramFinalText(value string) string {
	return mapMarkdownOutsideFences(value, func(line string) string {
		return markdownImagePattern.ReplaceAllStringFunc(line, func(match string) string {
			parts := markdownImagePattern.FindStringSubmatch(match)
			if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
				return "🖼 图片"
			}
			return "🖼 " + strings.TrimSpace(parts[1])
		})
	})
}

func mapMarkdownOutsideFences(value string, transform func(string) string) string {
	lines := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	inFence := false
	fenceMarker := ""
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		marker := ""
		switch {
		case strings.HasPrefix(trimmed, "```"):
			marker = "`"
		case strings.HasPrefix(trimmed, "~~~"):
			marker = "~"
		}
		if marker != "" {
			if !inFence {
				inFence = true
				fenceMarker = marker
			} else if marker == fenceMarker {
				inFence = false
				fenceMarker = ""
			}
			continue
		}
		if !inFence {
			lines[index] = transform(line)
		}
	}
	return strings.Join(lines, "\n")
}
