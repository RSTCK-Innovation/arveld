// Package notification owns persisted notification destinations.
package notification

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ErrNotFound means the requested notification channel does not exist.
var ErrNotFound = errors.New("notification channel not found")

// ErrInvalidChannel means the name, channel type or configuration is unsupported.
var ErrInvalidChannel = errors.New("invalid notification channel")

// ErrInUse means a rule still references this destination.
var ErrInUse = errors.New("notification channel is assigned to an alert rule")

// Channel describes a named destination with type-specific configuration.
// Saving a channel does not enable delivery.
type Channel struct {
	ID       string
	Name     string
	Type     string
	Config   json.RawMessage
	Delivery *DeliverySettings
}

// Store persists notification destinations in the controller database.
type Store struct {
	db *sql.DB
}

// NewStore requires a migrated database. The caller owns its closure.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Create validates and persists a channel without overwriting an existing ID.
func (store *Store) Create(ctx context.Context, value Channel) (Channel, error) {
	value, err := validate(value)
	if err != nil {
		return Channel{}, err
	}
	delivery, err := json.Marshal(value.Delivery)
	if err != nil {
		return Channel{}, fmt.Errorf("encode notification delivery settings: %w", err)
	}
	_, err = store.db.ExecContext(ctx, `INSERT INTO notification_channels (id, name, type, config, delivery) VALUES (?, ?, ?, ?, ?)`,
		value.ID, value.Name, value.Type, string(value.Config), string(delivery))
	if err != nil {
		return Channel{}, fmt.Errorf("create notification channel: %w", err)
	}
	return value, nil
}

// validate keeps common fields independent from each channel's native settings.
func validate(value Channel) (Channel, error) {
	value.Name = strings.TrimSpace(value.Name)
	length := utf8.RuneCountInString(value.Name)
	if !utf8.ValidString(value.Name) || length < 2 || length > 80 || strings.ContainsRune(value.Name, '\x00') {
		return Channel{}, ErrInvalidChannel
	}
	config, err := normalizeChannelConfig(value.Type, value.Config)
	if err != nil {
		return Channel{}, err
	}
	delivery, err := normalizeDelivery(value.Delivery)
	value.Delivery = delivery
	if err != nil {
		return Channel{}, err
	}
	value.Config = config
	return value, nil
}

func normalizeChannelConfig(channelType string, encoded json.RawMessage) (json.RawMessage, error) {
	if !json.Valid(encoded) {
		return nil, ErrInvalidChannel
	}
	if channelType == "email" {
		return normalizeEmailConfig(encoded)
	}
	if channelType == "telegram" {
		return normalizeTelegramConfig(encoded)
	}
	if channelType == "pagerduty" {
		return normalizePagerDutyConfig(encoded)
	}
	if channelType != "webhook" && channelType != "discord" && channelType != "slack" && channelType != "msteams" {
		return nil, ErrInvalidChannel
	}
	var config struct {
		URL string `json:"url"`
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return nil, ErrInvalidChannel
	}
	if err := validateWebhookURL(config.URL); err != nil {
		return nil, err
	}
	result, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("encode notification configuration: %w", err)
	}
	return result, nil
}

// validateWebhookURL accepts service webhooks and generic HTTP destinations.
func validateWebhookURL(rawURL string) error {
	if len(rawURL) > 2048 || strings.ContainsAny(rawURL, "# \t\r\n") {
		return ErrInvalidChannel
	}
	endpoint, err := url.Parse(rawURL)
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Hostname() == "" || endpoint.User != nil {
		return ErrInvalidChannel
	}
	if port := endpoint.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return ErrInvalidChannel
		}
	}
	return nil
}

// Get reads one channel or returns ErrNotFound.
func (store *Store) Get(ctx context.Context, id string) (Channel, error) {
	var value Channel
	var config, delivery string
	err := store.db.QueryRowContext(ctx, `SELECT id, name, type, config, delivery FROM notification_channels WHERE id = ?`, id).
		Scan(&value.ID, &value.Name, &value.Type, &config, &delivery)
	if errors.Is(err, sql.ErrNoRows) {
		return Channel{}, ErrNotFound
	}
	if err != nil {
		return Channel{}, fmt.Errorf("read notification channel: %w", err)
	}
	value.Config = json.RawMessage(config)
	value.Delivery, err = decodeDelivery(delivery)
	return value, err
}

// List reads all channels in stable ID order.
func (store *Store) List(ctx context.Context) (values []Channel, returnErr error) {
	rows, err := store.db.QueryContext(ctx, `SELECT id, name, type, config, delivery FROM notification_channels ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list notification channels: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close notification channels: %w", err))
		}
	}()
	values = make([]Channel, 0)
	for rows.Next() {
		var value Channel
		var config, delivery string
		if err := rows.Scan(&value.ID, &value.Name, &value.Type, &config, &delivery); err != nil {
			return nil, fmt.Errorf("scan notification channel: %w", err)
		}
		value.Config = json.RawMessage(config)
		value.Delivery, err = decodeDelivery(delivery)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read notification channels: %w", err)
	}
	return values, nil
}

// Update validates and replaces a channel's settings while preserving its ID.
// It returns ErrNotFound instead of creating an absent channel.
func (store *Store) Update(ctx context.Context, value Channel) (Channel, error) {
	value, err := validate(value)
	if err != nil {
		return Channel{}, err
	}
	delivery, err := json.Marshal(value.Delivery)
	if err != nil {
		return Channel{}, fmt.Errorf("encode notification delivery settings: %w", err)
	}
	result, err := store.db.ExecContext(ctx, `UPDATE notification_channels SET name = ?, type = ?, config = ?, delivery = ? WHERE id = ?`,
		value.Name, value.Type, string(value.Config), string(delivery), value.ID)
	if err != nil {
		return Channel{}, fmt.Errorf("update notification channel: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return Channel{}, fmt.Errorf("read notification update result: %w", err)
	}
	if count == 0 {
		return Channel{}, ErrNotFound
	}
	return value, nil
}

// Delete removes one channel or returns ErrNotFound.
func (store *Store) Delete(ctx context.Context, id string) error {
	result, err := store.db.ExecContext(ctx, `DELETE FROM notification_channels WHERE id = ?
		AND NOT EXISTS (SELECT 1 FROM alert_rule_notifications WHERE notification_id = ?)`, id, id)
	if err != nil {
		return fmt.Errorf("delete notification channel: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read notification deletion result: %w", err)
	}
	if count == 0 {
		if _, err := store.Get(ctx, id); err != nil {
			return err
		}
		return ErrInUse
	}
	return nil
}
