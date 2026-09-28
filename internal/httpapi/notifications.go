package httpapi

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/RSTCK-Innovation/arveld/internal/notification"
)

type notificationRequest struct {
	Name     string                         `json:"name"`
	Type     string                         `json:"type"`
	Config   json.RawMessage                `json:"config"`
	Delivery *notification.DeliverySettings `json:"delivery"`
}

type notificationResponse struct {
	ID       string                         `json:"id"`
	Name     string                         `json:"name"`
	Type     string                         `json:"type"`
	Config   json.RawMessage                `json:"config"`
	Delivery *notification.DeliverySettings `json:"delivery"`
}

func (api *api) createNotification(w http.ResponseWriter, r *http.Request) {
	var input notificationRequest
	if status := decodeJSONRequest(w, r, &input); status != 0 {
		fail(w, status)
		return
	}
	value, err := api.notifications.Create(r.Context(), notification.Channel{
		ID: rand.Text(), Name: input.Name, Type: input.Type, Config: input.Config, Delivery: input.Delivery,
	})
	if errors.Is(err, notification.ErrInvalidChannel) {
		fail(w, http.StatusUnprocessableEntity)
		return
	}
	if err != nil {
		api.internalError(w, r, "create notification channel", err)
		return
	}
	w.Header().Set("Location", "/api/v1/notifications/"+value.ID)
	writeJSON(w, http.StatusCreated, notificationJSON(value))
}

func (api *api) readNotification(w http.ResponseWriter, r *http.Request) {
	value, err := api.notifications.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, notification.ErrNotFound) {
		fail(w, http.StatusNotFound)
		return
	}
	if err != nil {
		api.internalError(w, r, "read notification channel", err)
		return
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return
	}
	writeJSON(w, http.StatusOK, notificationJSON(value))
}

func (api *api) listNotifications(w http.ResponseWriter, r *http.Request) {
	values, err := api.notifications.List(r.Context())
	if err != nil {
		api.internalError(w, r, "list notification channels", err)
		return
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return
	}
	response := make([]notificationResponse, 0, len(values))
	for _, value := range values {
		response = append(response, notificationJSON(value))
	}
	writeJSON(w, http.StatusOK, struct {
		Notifications []notificationResponse `json:"notifications"`
	}{Notifications: response})
}

func (api *api) updateNotification(w http.ResponseWriter, r *http.Request) {
	var input notificationRequest
	if status := decodeJSONRequest(w, r, &input); status != 0 {
		fail(w, status)
		return
	}
	value, err := api.notifications.Update(r.Context(), notification.Channel{
		ID: r.PathValue("id"), Name: input.Name, Type: input.Type, Config: input.Config, Delivery: input.Delivery,
	})
	if errors.Is(err, notification.ErrInvalidChannel) {
		fail(w, http.StatusUnprocessableEntity)
		return
	}
	if errors.Is(err, notification.ErrNotFound) {
		fail(w, http.StatusNotFound)
		return
	}
	if err != nil {
		api.internalError(w, r, "update notification channel", err)
		return
	}
	writeJSON(w, http.StatusOK, notificationJSON(value))
}

func (api *api) deleteNotification(w http.ResponseWriter, r *http.Request) {
	err := api.notifications.Delete(r.Context(), r.PathValue("id"))
	if errors.Is(err, notification.ErrInUse) {
		fail(w, http.StatusConflict)
		return
	}
	if errors.Is(err, notification.ErrNotFound) {
		fail(w, http.StatusNotFound)
		return
	}
	if err != nil {
		api.internalError(w, r, "delete notification channel", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func notificationJSON(value notification.Channel) notificationResponse {
	return notificationResponse{ID: value.ID, Name: value.Name, Type: value.Type, Config: value.Config, Delivery: value.Delivery}
}
