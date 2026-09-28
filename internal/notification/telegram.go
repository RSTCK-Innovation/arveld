package notification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// telegramConfig stores a bot destination, including an optional forum topic.
type telegramConfig struct {
	APIURL          string `json:"api_url"`
	BotToken        string `json:"bot_token"`
	ChatID          int64  `json:"chat_id"`
	MessageThreadID int64  `json:"message_thread_id"`
}

var telegramToken = regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]+$`)

func normalizeTelegramConfig(encoded json.RawMessage) (json.RawMessage, error) {
	config := telegramConfig{APIURL: "https://api.telegram.org"}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return nil, ErrInvalidChannel
	}
	// JavaScript numbers must retain the exact ID through an editor round trip.
	const maxSafeInteger = 9007199254740991
	if config.ChatID == 0 || config.ChatID < -maxSafeInteger || config.ChatID > maxSafeInteger || config.MessageThreadID < 0 || config.MessageThreadID > 2147483647 {
		return nil, ErrInvalidChannel
	}
	if len(config.BotToken) > 512 || !telegramToken.MatchString(config.BotToken) {
		return nil, ErrInvalidChannel
	}
	if err := validateWebhookURL(config.APIURL); err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(config.APIURL)
	if err != nil || endpoint.RawQuery != "" || endpoint.ForceQuery {
		return nil, ErrInvalidChannel
	}
	config.APIURL = strings.TrimRight(config.APIURL, "/")
	result, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("encode Telegram configuration: %w", err)
	}
	return result, nil
}
