package auth

import (
	"encoding/json"
	"net/http"
)

// loginRequest is the expected JSON body for POST /auth/login.
type loginRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

// loginResponse is returned on successful authentication.
type loginResponse struct {
	Token string `json:"token"`
}

// errorResponse is returned on any auth failure.
type errorResponse struct {
	Error string `json:"error"`
}

// Handler holds dependencies needed by auth HTTP handlers.
type Handler struct {
	service         *Service
	expectedLogin   string
	expectedPassword string
}

// NewHandler creates an auth Handler with credentials from config.
func NewHandler(svc *Service, login, password string) *Handler {
	return &Handler{
		service:          svc,
		expectedLogin:    login,
		expectedPassword: password,
	}
}

// Login handles POST /auth/login.
// It compares the provided credentials against the env-configured values and,
// on success, returns a signed JWT.
//
// @Summary      Login
// @Description  Autentica com login/senha e retorna um JWT
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      loginRequest   true  "Credenciais"
// @Success      200   {object}  loginResponse
// @Failure      400   {object}  errorResponse
// @Failure      401   {object}  errorResponse
// @Failure      500   {object}  errorResponse
// @Router       /auth/login [post]
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "invalid request body"})
		return
	}

	if req.Login != h.expectedLogin || req.Password != h.expectedPassword {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "invalid credentials"})
		return
	}

	token, err := h.service.GenerateToken(req.Login)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "could not generate token"})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(loginResponse{Token: token})
}
