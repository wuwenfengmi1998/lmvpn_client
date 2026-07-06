// Package auth implements the HTTP login flow (POST /api/login) to
// obtain a JWT for WebSocket authentication.
//
// (server: internal/handler/auth.go:32-82, internal/router/router.go:17)
package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// LoginResult holds the response from a successful /api/login call.
type LoginResult struct {
	Token string    `json:"token"`
	User  LoginUser `json:"user"`
}

// LoginUser is the user object embedded in the login response.
type LoginUser struct {
	ID       uint   `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// Login performs an HTTP POST to /api/login and returns the JWT.
//
// baseURL should be the HTTP(S) origin derived from the WebSocket URL
// (e.g. "http://localhost:8080" for ws://, "https://vpn.example.com"
// for wss://). See WSURLToHTTP.
func Login(baseURL, username, password string) (*LoginResult, error) {
	body, err := json.Marshal(loginRequest{Username: username, Password: password})
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(baseURL, "/") + "/api/login"
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("login request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read login response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var result LoginResult
		if err := json.Unmarshal(raw, &result); err != nil {
			return nil, fmt.Errorf("parse login response: %w", err)
		}
		return &result, nil
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusTooManyRequests, http.StatusInternalServerError:
		var e errorResponse
		_ = json.Unmarshal(raw, &e)
		return nil, &LoginError{Code: resp.StatusCode, Message: e.Error}
	default:
		return nil, &LoginError{Code: resp.StatusCode, Message: string(raw)}
	}
}

// LoginError carries the HTTP status code and server error message.
type LoginError struct {
	Code    int
	Message string
}

func (e *LoginError) Error() string {
	return fmt.Sprintf("login failed (%d): %s", e.Code, e.Message)
}

// IsRateLimited reports whether the error is a 429 rate-limit response.
func (e *LoginError) IsRateLimited() bool { return e.Code == http.StatusTooManyRequests }

// WSURLToHTTP converts a WebSocket URL to its HTTP origin.
//
//	ws://host:port/ws       → http://host:port
//	wss://host/ws           → https://host
//	ws://host:8080          → http://host:8080
func WSURLToHTTP(wsURL string) (string, error) {
	u := wsURL
	switch {
	case strings.HasPrefix(u, "wss://"):
		u = "https://" + u[len("wss://"):]
	case strings.HasPrefix(u, "ws://"):
		u = "http://" + u[len("ws://"):]
	default:
		return "", fmt.Errorf("invalid WebSocket URL: %s", wsURL)
	}
	// Strip the path (e.g. /ws) to get just the origin.
	if idx := strings.IndexByte(u, '/'); idx > 8 { // keep "https://"
		u = u[:idx]
	}
	return u, nil
}
