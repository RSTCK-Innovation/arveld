package httpapi

import (
	"net/http"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
)

type agentResponse struct {
	InstanceUID string     `json:"instance_uid"`
	Hostname    *string    `json:"hostname"`
	Version     *string    `json:"version"`
	Connected   bool       `json:"connected"`
	LastSeenAt  *time.Time `json:"last_seen_at"`
}

type agentsResponse struct {
	Agents []agentResponse `json:"agents"`
}

func newAgentsResponse(agents []agent.Agent) agentsResponse {
	response := agentsResponse{
		Agents: make([]agentResponse, 0, len(agents)),
	}

	for _, storedAgent := range agents {
		response.Agents = append(response.Agents, agentResponse{
			InstanceUID: storedAgent.InstanceUID.String(),
			Hostname:    storedAgent.Hostname,
			Version:     storedAgent.Version,
			Connected:   storedAgent.Connected,
			LastSeenAt:  storedAgent.LastSeenAt,
		})
	}

	return response
}

func (api *api) listAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := api.agents.List(r.Context())
	if err != nil {
		api.internalError(w, r, "list agents", err)
		return
	}

	writeJSON(w, http.StatusOK, newAgentsResponse(agents))
}
