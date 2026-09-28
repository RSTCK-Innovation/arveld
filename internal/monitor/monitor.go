package monitor

import (
	"github.com/RSTCK-Innovation/arveld/internal/agent"
)

// Monitor is a product definition and its Agent assignment, not compiled YAML.
// Validate enforces the selected protocol's settings; unused fields are zero.
type Monitor struct {
	HTTPOptions
	SkipTLSVerify    bool
	ID               string
	Name             string
	Protocol         string
	AgentInstanceUID agent.InstanceUID
	Endpoint         string
	Method           string
	IntervalSeconds  int
	TimeoutSeconds   int
	PingCount        int
	DNSServer        string
	RecordType       string
	Transport        string
}
