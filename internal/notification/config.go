package notification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

type alertmanagerRoute struct {
	Receiver       string              `yaml:"receiver"`
	GroupBy        []string            `yaml:"group_by,omitempty"`
	GroupWait      string              `yaml:"group_wait,omitempty"`
	GroupInterval  string              `yaml:"group_interval,omitempty"`
	RepeatInterval string              `yaml:"repeat_interval,omitempty"`
	Matchers       []string            `yaml:"matchers,omitempty"`
	Continue       bool                `yaml:"continue,omitempty"`
	Routes         []alertmanagerRoute `yaml:"routes,omitempty"`
}

type webhookConfig struct {
	URL string `json:"url" yaml:"url"`
}

type discordConfig struct {
	WebhookURL string `yaml:"webhook_url"`
	Title      string `yaml:"title"`
	Message    string `yaml:"message"`
	Username   string `yaml:"username"`
}

type teamsConfig struct {
	WebhookURL   string `yaml:"webhook_url"`
	SendResolved bool   `yaml:"send_resolved"`
	Title        string `yaml:"title"`
	Text         string `yaml:"text"`
}

type telegramReceiverConfig struct {
	Message         string `yaml:"message"`
	APIURL          string `yaml:"api_url"`
	BotToken        string `yaml:"bot_token"`
	ChatID          int64  `yaml:"chat_id"`
	MessageThreadID int64  `yaml:"message_thread_id,omitempty"`
	ParseMode       string `yaml:"parse_mode"`
	SendResolved    bool   `yaml:"send_resolved"`
}

type pagerDutyReceiverConfig struct {
	Description  string            `yaml:"description"`
	Details      map[string]string `yaml:"details"`
	Source       string            `yaml:"source"`
	ClientURL    string            `yaml:"client_url"`
	URL          string            `yaml:"url"`
	RoutingKey   string            `yaml:"routing_key"`
	Severity     string            `yaml:"severity"`
	Client       string            `yaml:"client"`
	SendResolved bool              `yaml:"send_resolved"`
}

type slackConfig struct {
	APIURL       string   `yaml:"api_url"`
	SendResolved bool     `yaml:"send_resolved"`
	Text         string   `yaml:"text"`
	Title        string   `yaml:"title"`
	TitleLink    string   `yaml:"title_link"`
	Fallback     string   `yaml:"fallback"`
	Footer       string   `yaml:"footer"`
	Username     string   `yaml:"username"`
	Color        string   `yaml:"color"`
	MarkdownIn   []string `yaml:"mrkdwn_in"`
}

type emailReceiverConfig struct {
	Headers          map[string]string `yaml:"headers"`
	HTML             string            `yaml:"html"`
	Text             string            `yaml:"text"`
	To               string            `yaml:"to"`
	From             string            `yaml:"from"`
	Smarthost        string            `yaml:"smarthost"`
	AuthUsername     string            `yaml:"auth_username,omitempty"`
	AuthPassword     string            `yaml:"auth_password,omitempty"`
	RequireTLS       bool              `yaml:"require_tls"`
	ForceImplicitTLS bool              `yaml:"force_implicit_tls"`
	SendResolved     bool              `yaml:"send_resolved"`
}

type alertmanagerReceiver struct {
	EmailConfigs     []emailReceiverConfig     `yaml:"email_configs,omitempty"`
	Name             string                    `yaml:"name"`
	WebhookConfigs   []webhookConfig           `yaml:"webhook_configs,omitempty"`
	DiscordConfigs   []discordConfig           `yaml:"discord_configs,omitempty"`
	SlackConfigs     []slackConfig             `yaml:"slack_configs,omitempty"`
	TeamsConfigs     []teamsConfig             `yaml:"msteamsv2_configs,omitempty"`
	TelegramConfigs  []telegramReceiverConfig  `yaml:"telegram_configs,omitempty"`
	PagerDutyConfigs []pagerDutyReceiverConfig `yaml:"pagerduty_configs,omitempty"`
}

// AlertmanagerConfig renders destinations and exact rule-to-channel routes.
// Alerts without an explicit assignment use the empty default receiver.
func (store *Store) AlertmanagerConfig(ctx context.Context) (content []byte, returnErr error) {
	// One SQLite snapshot prevents a route from referring to a newly created
	// channel that was absent from an earlier receiver read.
	rows, err := store.db.QueryContext(ctx, `SELECT c.id,c.name,c.type,c.config,c.delivery,
 (SELECT json_group_array(rule_id) FROM (SELECT rule_id FROM alert_rule_notifications WHERE notification_id=c.id ORDER BY rule_id))
 FROM notification_channels c ORDER BY c.id`)
	if err != nil {
		return nil, fmt.Errorf("read notification configuration: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close notification configuration: %w", err))
		}
	}()
	config := struct {
		Route     alertmanagerRoute      `yaml:"route"`
		Receivers []alertmanagerReceiver `yaml:"receivers"`
	}{
		Route:     alertmanagerRoute{Receiver: "arveld"},
		Receivers: []alertmanagerReceiver{{Name: "arveld"}},
	}
	for rows.Next() {
		var channel Channel
		var encoded, delivery, assigned string
		if err := rows.Scan(&channel.ID, &channel.Name, &channel.Type, &encoded, &delivery, &assigned); err != nil {
			return nil, fmt.Errorf("scan notification configuration: %w", err)
		}
		channel.Config = json.RawMessage(encoded)
		channel.Delivery, err = decodeDelivery(delivery)
		if err != nil {
			return nil, err
		}
		var ruleIDs []string
		if err := json.Unmarshal([]byte(assigned), &ruleIDs); err != nil {
			return nil, fmt.Errorf("decode notification assignments: %w", err)
		}
		channel, err = validate(channel)
		if err != nil {
			return nil, fmt.Errorf("validate stored notification channel: %w", err)
		}
		destination, err := channelReceiver(channel)
		if err != nil {
			return nil, err
		}
		config.Receivers = append(config.Receivers, destination)
		config.Route.Routes = append(config.Route.Routes, channelRoutes(destination.Name, ruleIDs, *channel.Delivery)...)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate notification configuration: %w", err)
	}
	if len(config.Route.Routes) > 0 {
		config.Route.GroupBy = []string{"arveld_rule_id", "arveld_rule_revision"}
		config.Route.GroupWait = "5s"
		config.Route.GroupInterval = "30s"
		config.Route.RepeatInterval = "4h"
	}
	content, err = yaml.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("encode Alertmanager configuration: %w", err)
	}
	return content, nil
}

// channelReceiver translates the validated product contract into a native receiver.
func channelReceiver(channel Channel) (alertmanagerReceiver, error) {
	destination := alertmanagerReceiver{Name: "arveld-channel-" + channel.ID}
	if channel.Type == "pagerduty" {
		var config pagerDutyConfig
		if err := json.Unmarshal(channel.Config, &config); err != nil {
			return alertmanagerReceiver{}, fmt.Errorf("decode stored PagerDuty configuration: %w", err)
		}
		destination.PagerDutyConfigs = []pagerDutyReceiverConfig{{
			URL: config.URL, RoutingKey: config.RoutingKey, Client: "Arveld", SendResolved: true,
			Description: notificationTitle,
			// Alertmanager merges its default detail keys, so explicitly replace
			// the two raw alert dumps as well as adding the readable message.
			Details: map[string]string{"message": notificationBody, "firing": `{{ len .Alerts.Firing }} active`, "resolved": `{{ len .Alerts.Resolved }} recovered`},
			Source:  `{{ if .CommonAnnotations.resource }}{{ .CommonAnnotations.resource }}{{ else }}Arveld{{ end }}`, ClientURL: "",
			Severity: `{{ $severity := "warning" }}{{ range .Alerts }}{{ if eq .Labels.severity "critical" }}{{ $severity = "critical" }}{{ end }}{{ end }}{{ $severity }}`,
		}}
		return destination, nil
	}
	if channel.Type == "telegram" {
		var config telegramConfig
		if err := json.Unmarshal(channel.Config, &config); err != nil {
			return alertmanagerReceiver{}, fmt.Errorf("decode stored Telegram configuration: %w", err)
		}
		destination.TelegramConfigs = []telegramReceiverConfig{{
			Message: notificationText,
			APIURL:  config.APIURL, BotToken: config.BotToken, ChatID: config.ChatID,
			MessageThreadID: config.MessageThreadID, ParseMode: "", SendResolved: true,
		}}
		return destination, nil
	}
	if channel.Type == "email" {
		var config emailConfig
		if err := json.Unmarshal(channel.Config, &config); err != nil {
			return alertmanagerReceiver{}, fmt.Errorf("decode stored email configuration: %w", err)
		}
		destination.EmailConfigs = []emailReceiverConfig{{
			Headers: map[string]string{"Subject": notificationTitle}, HTML: emailHTML, Text: notificationText,
			To: config.To, From: config.From, Smarthost: config.Smarthost,
			AuthUsername: config.AuthUsername, AuthPassword: config.AuthPassword,
			RequireTLS: config.TLSMode != "none", ForceImplicitTLS: config.TLSMode == "tls", SendResolved: true,
		}}
		return destination, nil
	}
	var webhook webhookConfig
	if err := json.Unmarshal(channel.Config, &webhook); err != nil {
		return alertmanagerReceiver{}, fmt.Errorf("decode stored webhook configuration: %w", err)
	}
	switch channel.Type {
	case "msteams":
		destination.TeamsConfigs = []teamsConfig{{WebhookURL: webhook.URL, SendResolved: true, Title: notificationTitle, Text: notificationBody}}
	case "discord":
		destination.DiscordConfigs = []discordConfig{{WebhookURL: webhook.URL, Title: notificationTitle, Message: notificationBody, Username: "Arveld"}}
	case "slack":
		destination.SlackConfigs = []slackConfig{{
			APIURL: webhook.URL, SendResolved: true,
			Text: notificationBody, Title: notificationTitle, Fallback: notificationTitle,
			TitleLink: "", Footer: "Arveld", Username: "Arveld", MarkdownIn: []string{},
			Color: `{{ if eq .Status "resolved" }}#28704d{{ else }}#b03930{{ end }}`,
		}}
	case "webhook":
		destination.WebhookConfigs = []webhookConfig{webhook}
	}
	return destination, nil
}

// channelRoutes keeps the existing rule routes unless resource grouping is requested.
func channelRoutes(receiver string, ruleIDs []string, settings DeliverySettings) []alertmanagerRoute {
	if len(ruleIDs) == 0 {
		return nil
	}
	route := alertmanagerRoute{Receiver: receiver, Continue: true}
	defaults := defaultDeliverySettings()
	if settings.GroupWaitSeconds != defaults.GroupWaitSeconds {
		route.GroupWait = strconv.Itoa(settings.GroupWaitSeconds) + "s"
	}
	if settings.GroupIntervalSeconds != defaults.GroupIntervalSeconds {
		route.GroupInterval = strconv.Itoa(settings.GroupIntervalSeconds) + "s"
	}
	if settings.RepeatIntervalSeconds != defaults.RepeatIntervalSeconds {
		route.RepeatInterval = strconv.Itoa(settings.RepeatIntervalSeconds) + "s"
	}
	if settings.GroupBy == "resource" {
		escaped := make([]string, len(ruleIDs))
		for i, id := range ruleIDs {
			escaped[i] = regexp.QuoteMeta(id)
		}
		route.Matchers = []string{"arveld_rule_id=~" + strconv.Quote("^("+strings.Join(escaped, "|")+")$")}
		// An Agent-owned alert has no Monitor ID; it must not group with its Monitors.
		route.GroupBy = []string{"arveld_agent_id", "arveld_monitor_id"}
		return []alertmanagerRoute{route}
	}
	routes := make([]alertmanagerRoute, 0, len(ruleIDs))
	for _, id := range ruleIDs {
		child := route
		child.Matchers = []string{"arveld_rule_id=" + strconv.Quote(id)}
		routes = append(routes, child)
	}
	return routes
}
