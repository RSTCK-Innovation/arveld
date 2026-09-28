package notification

import (
	"encoding/json"
	"fmt"
)

// DeliverySettings controls native notification grouping and timing for one channel.
// Durations are whole seconds; Alertmanager owns the corresponding timers.
type DeliverySettings struct {
	GroupBy               string `json:"group_by"`
	GroupWaitSeconds      int    `json:"group_wait_seconds"`
	GroupIntervalSeconds  int    `json:"group_interval_seconds"`
	RepeatIntervalSeconds int    `json:"repeat_interval_seconds"`
}

func defaultDeliverySettings() DeliverySettings {
	return DeliverySettings{GroupBy: "rule", GroupWaitSeconds: 5, GroupIntervalSeconds: 30, RepeatIntervalSeconds: 14400}
}

func normalizeDelivery(value *DeliverySettings) (*DeliverySettings, error) {
	settings := defaultDeliverySettings()
	if value != nil {
		settings = *value
	}
	if settings.GroupBy != "rule" && settings.GroupBy != "resource" {
		return nil, ErrInvalidChannel
	}
	if settings.GroupWaitSeconds < 0 || settings.GroupWaitSeconds > 3600 || settings.GroupIntervalSeconds < 1 || settings.GroupIntervalSeconds > 86400 {
		return nil, ErrInvalidChannel
	}
	// The engine checks repeats on group intervals. Require exact multiples so
	// the configured duration is not silently rounded. Its retention is five days.
	if settings.RepeatIntervalSeconds < settings.GroupIntervalSeconds || settings.RepeatIntervalSeconds > 432000 || settings.RepeatIntervalSeconds%settings.GroupIntervalSeconds != 0 {
		return nil, ErrInvalidChannel
	}
	return &settings, nil
}

func decodeDelivery(encoded string) (*DeliverySettings, error) {
	var settings DeliverySettings
	if err := json.Unmarshal([]byte(encoded), &settings); err != nil {
		return nil, fmt.Errorf("decode notification delivery settings: %w", err)
	}
	return normalizeDelivery(&settings)
}
