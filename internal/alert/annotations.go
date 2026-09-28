package alert

import (
	"strconv"
	"time"
)

// annotations describes the condition without changing its routing identity.
// Names are quoted as template literals so user text cannot become template code.
func (value definition) annotations() map[string]string {
	resource := value.ownerName
	if resource == "" {
		resource = "Monitor"
		if value.rule.AgentInstanceUID != nil {
			resource = "Agent"
		}
	}
	summary, description := "Check failed", "The latest check did not meet this Monitor's success criteria."
	threshold := ""
	if value.rule.Threshold != nil {
		threshold = strconv.FormatFloat(*value.rule.Threshold, 'f', -1, 64)
	}
	switch value.rule.Condition {
	case "no_data":
		summary, description = "No recent results", "No recent check result is available. Check that the Agent is connected and the Monitor is running."
	case "latency":
		summary = "Slow response"
		description = `Response time is {{ printf "%.0f" $value }} ms (threshold: ` + threshold + " ms)."
	case "cpu", "memory", "disk":
		metric := map[string]string{"cpu": "CPU", "memory": "Memory", "disk": "Disk"}[value.rule.Condition]
		summary = "High " + metric + " usage"
		description = metric + ` usage is {{ printf "%.1f" $value }}% (threshold: ` + threshold + "%)."
	}
	if value.rule.ForSeconds > 0 {
		description += " Alert delay: " + (time.Duration(value.rule.ForSeconds) * time.Second).String() + "."
	}
	return map[string]string{
		"resource":    "{{ " + strconv.Quote(resource) + " }}",
		"summary":     summary,
		"description": description,
	}
}
