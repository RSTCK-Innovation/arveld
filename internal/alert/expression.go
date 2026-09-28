package alert

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/RSTCK-Innovation/arveld/internal/monitor"
)

// conditionExpression selects a known failure or the absence of a usable result.
// Inactive describes the selected condition, not the overall Monitor health.
// The owned engine lookback covers the maximum Monitor freshness window.
// Each rule still filters by its own interval and timeout.
func conditionExpression(owner monitor.Monitor, assertions int, condition string, threshold *float64) (string, error) {
	var metric, durationMetric string
	maximum := 1
	switch owner.Protocol {
	case "http":
		metric, durationMetric = "httpcheck_status", "httpcheck_duration_milliseconds"
	case "tcp":
		metric, durationMetric = "tcpcheck_status_ratio", "tcpcheck_duration_milliseconds"
	case "icmp":
		metric, durationMetric, maximum = "ping_loss_ratio_percent", "ping_rtt_avg_milliseconds", 100
	case "dns":
		metric, durationMetric = "dnscheck_status", "dnscheck_duration_milliseconds"
	default:
		return "", errors.New("unsupported Monitor alert protocol")
	}
	freshness := max(60, 2*owner.IntervalSeconds+owner.TimeoutSeconds)
	labels := fmt.Sprintf(`job="arveld-agent",instance=%q,arveld_monitor_id=%q`, owner.AgentInstanceUID.String(), owner.ID)
	raw := metric + "{" + labels + "}"
	latest := "max(timestamp(" + raw + "))"
	recent := fmt.Sprintf("(%s > time() - %d) and (%s <= time())", latest, freshness, latest)
	selected := raw
	if owner.Protocol == "http" {
		selected = metric + "{" + labels + `,http_status_class=~"[23]xx"}`
	}
	current := fmt.Sprintf("(%s and (timestamp(%s) == scalar(%s)))", selected, selected, recent)
	valid := fmt.Sprintf("(%s >= 0) and (%s <= %d)", current, current, maximum)
	if condition == "latency" {
		if threshold == nil {
			return "", ErrInvalidRule
		}
		duration := durationMetric + "{" + labels + "}"
		// A duration belongs to the newest status measurement, never an older attempt.
		latency := fmt.Sprintf("max((%s >= 0) and (%s < +Inf) and (timestamp(%s) == scalar(%s)))", duration, duration, duration, recent)
		if owner.Protocol == "icmp" {
			latency = "(" + latency + ") and (max(" + valid + ") < 100)"
		}
		return "(" + latency + ") > " + strconv.FormatFloat(*threshold, 'g', -1, 64), nil
	}
	success := "min(" + valid + ")"
	switch owner.Protocol {
	case "http":
		success = "max(" + valid + ")"
	case "icmp":
		success = "(max(" + valid + ") == bool 0)"
	}
	if owner.Protocol == "http" && assertions > 0 {
		count := func(outcome string) string {
			series := "httpcheck_validation_" + outcome + "{" + labels + "}"
			// >= filters while preserving counts, including repeated validation types.
			return fmt.Sprintf("(sum((%s >= 0) and (timestamp(%s) == scalar(%s))) or vector(0))", series, series, recent)
		}
		passed, failed := count("passed"), count("failed")
		// A known HTTP/assertion failure wins. Success needs complete assertion coverage.
		success = fmt.Sprintf("(%s == 0) or ((%s) * ((vector(0) and (%s > 0)) or (vector(1) and (%s == %d) and (%s == 0))))",
			success, success, failed, passed, assertions, failed)
	}
	switch condition {
	case "failed":
		return "(" + success + ") == 0", nil
	case "no_data":
		// A usable result is either known failure (0) or known success (1).
		return "absent(((" + success + ") == 0) or ((" + success + ") == 1))", nil
	default:
		return "", ErrInvalidRule
	}
}
