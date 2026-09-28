package alert

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
)

// agentConditionExpression follows the resource measurements displayed by Arveld.
// Aggregate before applying the threshold so a volume/CPU label change keeps one alert.
func agentConditionExpression(uid agent.InstanceUID, condition string, threshold *float64) (string, error) {
	if threshold == nil {
		return "", ErrInvalidRule
	}
	labels := fmt.Sprintf(`job="arveld-agent",instance=%q`, uid.String())
	fresh := func(metric string) string {
		return "(timestamp(" + metric + ") > time() - 60) and (timestamp(" + metric + ") <= time())"
	}
	var usage string
	switch condition {
	case "cpu":
		total := "system_cpu_time_seconds_total{" + labels + "}"
		idle := "system_cpu_time_seconds_total{" + labels + `,state="idle"}`
		rate := func(metric string) string { return "sum(rate(" + metric + "[2m]) and " + fresh(metric) + ")" }
		usage = "clamp(100 * (1 - " + rate(idle) + " / (" + rate(total) + " > 0)), 0, 100)"
	case "memory":
		available := "system_linux_memory_available_bytes{" + labels + "}"
		total := "system_memory_limit_bytes{" + labels + "}"
		usage = "max(" + strings.Join([]string{
			"100 * (1 - " + available + " / (" + total + " > 0))",
			"(" + available + " >= 0)", "(" + available + " <= " + total + ")",
			"(" + total + " < +Inf)", fresh(available), fresh(total),
		}, " and ") + ")"
	case "disk":
		used := "system_filesystem_usage_bytes{" + labels + `,mode="rw",state="used"}`
		free := "system_filesystem_usage_bytes{" + labels + `,mode="rw",state="free"}`
		capacity := "(" + used + " + ignoring(state) " + free + ")"
		// Reserved space is not available to applications, matching the disk graph.
		usage = "max(" + strings.Join([]string{
			"100 * (" + used + " / ignoring(state) (" + capacity + " > 0))",
			"(" + used + " >= 0)", "(" + free + " >= 0)", "(" + capacity + " < +Inf)",
			"(timestamp(" + used + ") > time() - 60)", "(timestamp(" + used + ") <= time())",
			"(timestamp(" + free + ") > time() - 60)", "(timestamp(" + free + ") <= time())",
		}, " and ignoring(state) ") + ")"
	default:
		return "", ErrInvalidRule
	}
	return "(" + usage + ") > " + strconv.FormatFloat(*threshold, 'g', -1, 64), nil
}
