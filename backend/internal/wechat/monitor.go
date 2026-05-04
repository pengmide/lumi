package wechat

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"strings"
	"time"
)

func (s *Service) runMonitorLoop(ctx context.Context, cfg Config, done chan struct{}) {
	defer close(done)
	defer func() {
		if err := s.updateRuntime(func(state *RuntimeState) {
			state.Running = false
		}); err != nil {
			log.Printf("wechat: failed to persist stopped state: %v", err)
		}
	}()

	client := NewClient(cfg)
	state, err := s.runtimeStore.Load()
	if err != nil {
		log.Printf("wechat: failed to load runtime state: %v", err)
		state = DefaultRuntimeState()
	}

	consecutiveFailures := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		resp, err := client.GetUpdates(ctx, state.Buf)
		if err != nil {
			consecutiveFailures++
			_ = s.runtimeStore.Save(state)
			_ = s.updateRuntime(func(runtime *RuntimeState) {
				runtime.LastError = err.Error()
				runtime.Running = true
			})
			delay := 2 * time.Second
			if consecutiveFailures >= 3 {
				delay = 30 * time.Second
			}
			if sleepContext(ctx, delay) != nil {
				return
			}
			continue
		}

		consecutiveFailures = 0
		state.LastError = ""
		state.LastSyncAt = time.Now().UnixMilli()
		if resp.Buf != "" {
			state.Buf = resp.Buf
		}
		if err := s.runtimeStore.Save(state); err != nil {
			log.Printf("wechat: failed to persist runtime state: %v", err)
		}

		for _, raw := range resp.Messages {
			inbound, ok := parseInboundMessage(raw)
			if !ok {
				continue
			}

			dedupeKey := inbound.MessageID
			if dedupeKey == "" {
				dedupeKey = inbound.ConversationKey + ":" + strconv.FormatInt(inbound.ReceivedAt, 10)
			}
			if containsProcessedID(state.ProcessedMessageIDs, dedupeKey) {
				continue
			}

			if err := s.handleInboundMessage(ctx, cfg, inbound); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("wechat: inbound handling failed: %v", err)
				state.LastError = err.Error()
			}
			state.LastMessageAt = time.Now().UnixMilli()
			state.ProcessedMessageIDs = append(state.ProcessedMessageIDs, dedupeKey)
			state.ProcessedMessageIDs = trimProcessedMessageIDs(state.ProcessedMessageIDs)
			if err := s.runtimeStore.Save(state); err != nil {
				log.Printf("wechat: failed to persist processed message state: %v", err)
			}
		}
	}
}

func parseInboundMessage(raw rawMessage) (WeChatInboundMessage, bool) {
	conversationKey := strings.TrimSpace(raw.FromUserID)
	if conversationKey == "" {
		log.Printf("wechat: drop message without from_user_id: %s", mustJSON(raw))
		return WeChatInboundMessage{}, false
	}

	textParts := make([]string, 0)
	attachments := make([]WeChatAttachment, 0)
	for _, item := range raw.ItemList {
		switch {
		case item.TextItem != nil || item.Type == wechatMsgText:
			if item.TextItem != nil && strings.TrimSpace(item.TextItem.Text) != "" {
				textParts = append(textParts, strings.TrimSpace(item.TextItem.Text))
			}
		case item.VoiceItem != nil || item.Type == wechatMsgVoice:
			if item.VoiceItem != nil && strings.TrimSpace(item.VoiceItem.Text) != "" {
				textParts = append(textParts, strings.TrimSpace(item.VoiceItem.Text))
			}
		case item.ImageItem != nil || item.Type == wechatMsgImage:
			if attachment, ok := newInboundAttachment("image", item.ImageItem); ok {
				attachments = append(attachments, attachment)
			}
		case item.FileItem != nil || item.Type == wechatMsgFile:
			if attachment, ok := newInboundAttachment("file", item.FileItem); ok {
				attachments = append(attachments, attachment)
			}
		}
	}

	return WeChatInboundMessage{
		ConversationKey: conversationKey,
		MessageID:       strings.TrimSpace(raw.MessageID),
		ContextToken:    strings.TrimSpace(raw.ContextToken),
		Text:            strings.Join(textParts, "\n\n"),
		Attachments:     attachments,
		ReceivedAt:      time.Now().UnixMilli(),
	}, len(textParts) > 0 || len(attachments) > 0
}

func newInboundAttachment(kind string, media *rawMediaItem) (WeChatAttachment, bool) {
	if media == nil || media.Media == nil || strings.TrimSpace(media.Media.EncryptQueryParam) == "" {
		return WeChatAttachment{}, false
	}
	size := int64(0)
	if media.Len != "" {
		if parsed, err := strconv.ParseInt(media.Len, 10, 64); err == nil {
			size = parsed
		}
	}
	name := strings.TrimSpace(media.FileName)
	if name == "" {
		name = kind
	}
	return WeChatAttachment{
		Kind:          kind,
		Name:          name,
		Path:          strings.TrimSpace(media.Media.EncryptQueryParam),
		Size:          size,
		downloadQuery: strings.TrimSpace(media.Media.EncryptQueryParam),
		aesKeyHex:     strings.TrimSpace(media.AESKey),
		aesKeyBase64:  strings.TrimSpace(media.Media.AESKey),
	}, true
}

func containsProcessedID(ids []string, target string) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "<marshal error>"
	}
	return string(data)
}
