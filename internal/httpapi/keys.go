package httpapi

import (
	"errors"
	"net/http"

	"github.com/RSTCK-Innovation/arveld/internal/agentauth"
	"github.com/RSTCK-Innovation/arveld/internal/auth"
)

type createAPIKeyRequest struct {
	Name        string `json:"name"`
	Permission  string `json:"permission"`
	ExpiresDays *int   `json:"expiresDays"`
}

type createAPIKeyResponse struct {
	Key   auth.APIKey `json:"key"`
	Token string      `json:"token"`
}

type apiKeysResponse struct {
	Keys []auth.APIKey `json:"keys"`
}

func (api *api) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := api.auth.ListAPIKeys(r.Context())
	if err != nil {
		api.internalError(w, r, "list API keys", err)
		return
	}
	writeJSON(w, http.StatusOK, apiKeysResponse{Keys: keys})
}

func (api *api) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	if err := api.auth.RevokeAPIKey(r.Context(), r.PathValue("id")); err != nil {
		if errors.Is(err, auth.ErrAPIKeyNotFound) {
			fail(w, http.StatusNotFound)
			return
		}
		api.internalError(w, r, "revoke API key", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *api) createAPIKey(w http.ResponseWriter, r *http.Request) {
	// An omitted field keeps the invalid zero; explicit JSON null means no expiration.
	input := createAPIKeyRequest{ExpiresDays: new(int)}
	if status := decodeJSONRequest(w, r, &input); status != 0 {
		fail(w, status)
		return
	}
	params, err := auth.ValidateCreateAPIKey(auth.CreateAPIKeyParams{
		Name: input.Name, Permission: input.Permission, ExpiresDays: input.ExpiresDays,
	})
	if err != nil {
		fail(w, http.StatusUnprocessableEntity)
		return
	}
	key, token, err := api.auth.CreateAPIKey(r.Context(), params)
	if err != nil {
		api.internalError(w, r, "create API key", err)
		return
	}
	writeJSON(w, http.StatusCreated, createAPIKeyResponse{Key: key, Token: token})
}

type createAgentKeyRequest struct {
	Name string `json:"name"`
}

type createAgentKeyResponse struct {
	Key   agentauth.AgentKey `json:"key"`
	Token string             `json:"token"`
}

type agentKeysResponse struct {
	Keys []agentauth.AgentKey `json:"keys"`
}

func (api *api) listAgentKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := api.agentKeys.ListKeys(r.Context())
	if err != nil {
		api.internalError(w, r, "list agent keys", err)
		return
	}
	writeJSON(w, http.StatusOK, agentKeysResponse{Keys: keys})
}

func (api *api) revokeAgentKey(w http.ResponseWriter, r *http.Request) {
	if err := api.agentKeys.RevokeKey(r.Context(), r.PathValue("id")); err != nil {
		if errors.Is(err, agentauth.ErrKeyNotFound) {
			fail(w, http.StatusNotFound)
			return
		}
		api.internalError(w, r, "revoke agent key", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *api) createAgentKey(w http.ResponseWriter, r *http.Request) {
	var input createAgentKeyRequest
	if status := decodeJSONRequest(w, r, &input); status != 0 {
		fail(w, status)
		return
	}
	params, err := agentauth.ValidateCreateKey(agentauth.CreateKeyParams{Name: input.Name})
	if err != nil {
		fail(w, http.StatusUnprocessableEntity)
		return
	}
	key, token, err := api.agentKeys.CreateKey(r.Context(), params)
	if err != nil {
		api.internalError(w, r, "create agent key", err)
		return
	}
	writeJSON(w, http.StatusCreated, createAgentKeyResponse{Key: key, Token: token})
}
