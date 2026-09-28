package httpapi

import (
	"errors"
	"net/http"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/RSTCK-Innovation/arveld/internal/auth"
)

type setupResponse struct {
	Required bool `json:"required"`
}

func (api *api) setupStatus(w http.ResponseWriter, r *http.Request) {
	required, err := api.auth.NeedsSetup(r.Context())
	if err != nil {
		api.internalError(w, r, "read setup state", err)
		return
	}

	writeJSON(w, http.StatusOK, setupResponse{Required: required})
}

type createAdministratorRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (api *api) createAdministrator(w http.ResponseWriter, r *http.Request) {
	var input createAdministratorRequest
	if status := decodeJSONRequest(w, r, &input); status != 0 {
		fail(w, status)
		return
	}
	params, err := auth.ValidateCreateAdministrator(auth.CreateAdministratorParams{Name: input.Name, Email: input.Email, Password: input.Password})
	if err != nil {
		fail(w, http.StatusUnprocessableEntity)
		return
	}
	if !api.setupAdmission.acquire() {
		tooManyRequests(w)
		return
	}
	defer api.setupAdmission.release()

	if err := api.auth.CreateAdministrator(r.Context(), params); err != nil {
		if errors.Is(err, auth.ErrAdministratorExists) {
			fail(w, http.StatusConflict)
			return
		}
		api.internalError(w, r, "create administrator", err)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (api *api) login(w http.ResponseWriter, r *http.Request) {
	var input loginRequest
	if status := decodeJSONRequest(w, r, &input); status != 0 {
		fail(w, status)
		return
	}
	if !api.loginAdmission.acquire() {
		tooManyRequests(w)
		return
	}
	defer api.loginAdmission.release()
	ctx := r.Context()
	verified, err := api.auth.Authenticate(ctx, input.Email, input.Password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		fail(w, http.StatusUnauthorized)
		return
	}
	if err != nil {
		api.internalError(w, r, "verify login credentials", err)
		return
	}
	if err := api.browser.start(w, r, verified); err != nil {
		if errors.Is(err, auth.ErrCredentialsChanged) {
			fail(w, http.StatusUnauthorized)
			return
		}
		api.internalError(w, r, "start browser session", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type sessionResponse struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func (api *api) currentSession(w http.ResponseWriter, r *http.Request) {
	administrator, ok := api.browserAdministrator(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, sessionResponse{Name: administrator.Name, Email: administrator.Email})
}

func (api *api) logout(w http.ResponseWriter, r *http.Request) {
	if err := api.browser.end(w, r); err != nil {
		api.internalError(w, r, "end browser session", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type updateProfileRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func (api *api) updateProfile(w http.ResponseWriter, r *http.Request) {
	var input updateProfileRequest
	if status := decodeJSONRequest(w, r, &input); status != 0 {
		fail(w, status)
		return
	}
	params, err := auth.ValidateUpdateProfile(auth.UpdateProfileParams{Name: input.Name, Email: input.Email})
	if err != nil {
		fail(w, http.StatusUnprocessableEntity)
		return
	}
	if err := api.auth.UpdateProfile(r.Context(), params); err != nil {
		if errors.Is(err, auth.ErrAdministratorNotFound) {
			fail(w, http.StatusUnauthorized)
			return
		}
		api.internalError(w, r, "update administrator profile", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	Password        string `json:"password"`
}

func (api *api) changePassword(w http.ResponseWriter, r *http.Request) {
	var input changePasswordRequest
	if status := decodeJSONRequest(w, r, &input); status != 0 {
		fail(w, status)
		return
	}
	params := auth.ChangePasswordParams{CurrentPassword: input.CurrentPassword, Password: input.Password}
	if err := auth.ValidateChangePassword(params); err != nil {
		fail(w, http.StatusUnprocessableEntity)
		return
	}
	if !api.passwordAdmission.acquire() {
		tooManyRequests(w)
		return
	}
	defer api.passwordAdmission.release()
	ctx := r.Context()
	administrator, ok := api.browserAdministrator(w, r)
	if !ok {
		return
	}
	if err := api.auth.ChangePassword(ctx, administrator, params); err != nil {
		if errors.Is(err, auth.ErrCredentialsChanged) || errors.Is(err, auth.ErrInvalidCredentials) {
			fail(w, http.StatusForbidden)
			return
		}
		api.internalError(w, r, "change administrator password", err)
		return
	}
	api.browser.expireCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// admissionGate belongs to one operation on one router. Rejected work never queues.
type admissionGate struct {
	busy    atomic.Bool
	limiter *rate.Limiter
}

func newAdmission(interval time.Duration) *admissionGate {
	gate := &admissionGate{}
	if interval > 0 {
		gate.limiter = rate.NewLimiter(rate.Every(interval), 1)
	}
	return gate
}

func (gate *admissionGate) acquire() bool {
	if !gate.busy.CompareAndSwap(false, true) {
		return false
	}
	if gate.limiter != nil && !gate.limiter.Allow() {
		gate.busy.Store(false)
		return false
	}
	return true
}
func (gate *admissionGate) release() { gate.busy.Store(false) }
