package handler

import (
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/aadityya4real/Url-shortener-service/internal/model"
	"github.com/aadityya4real/Url-shortener-service/internal/repository"
	"github.com/aadityya4real/Url-shortener-service/internal/service"
	"github.com/aadityya4real/Url-shortener-service/pkg/response"
)

//go:embed static
var staticEmbedFS embed.FS

type Handler struct {
	service      *service.LinkService
	userService  *service.UserService
	db           *sql.DB
	baseURL      string
	maxBodyBytes int64
}

type createRequest struct {
	URL              string `json:"url"`
	CustomAlias      string `json:"custom_alias,omitempty"`
	ExpiresInSeconds int64  `json:"expires_in_seconds,omitempty"`
}

type linkResponse struct {
	ID          int64      `json:"id"`
	Code        string     `json:"code"`
	ShortURL    string     `json:"short_url"`
	OriginalURL string     `json:"original_url"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	Visits      int64      `json:"visits"`
}

type conflictResponse struct {
	Error    string `json:"error"`
	ShortURL string `json:"short_url"`
}

type authRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func New(service *service.LinkService, userService *service.UserService, db *sql.DB, baseURL string, maxBodyBytes int64) *Handler {
	return &Handler{
		service:      service,
		userService:  userService,
		db:           db,
		baseURL:      strings.TrimRight(baseURL, "/"),
		maxBodyBytes: maxBodyBytes,
	}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", h.home)
	mux.HandleFunc("GET /favicon.ico", h.favicon)
	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("GET /readyz", h.ready)
	mux.HandleFunc("POST /api/v1/urls", h.create)
	mux.HandleFunc("GET /api/v1/urls/{code}", h.get)
	mux.HandleFunc("DELETE /api/v1/urls/{code}", h.delete)
	mux.HandleFunc("GET /{code}", h.redirect)

	// Auth Endpoints
	mux.HandleFunc("POST /api/v1/signup", h.signUp)
	mux.HandleFunc("POST /api/v1/login", h.logIn)
	mux.HandleFunc("POST /api/v1/logout", h.logOut)
	mux.HandleFunc("GET /api/v1/me", h.me)
	mux.HandleFunc("GET /api/v1/user/urls", h.userURLs)

	// Static Files
	mux.Handle("GET /static/", http.FileServer(http.FS(staticEmbedFS)))

	return mux
}

func (h *Handler) home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		response.Error(w, http.StatusNotFound, "page not found")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, homePage)
}

func (h *Handler) favicon(w http.ResponseWriter, r *http.Request) {
	data, err := staticEmbedFS.ReadFile("static/assets/favicon.ico")
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "image/x-icon")
	_, _ = w.Write(data)
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	response.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ready(w http.ResponseWriter, r *http.Request) {
	if err := h.db.PingContext(r.Context()); err != nil {
		response.Error(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	response.JSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (h *Handler) getSessionUser(r *http.Request) (*model.User, error) {
	cookie, err := r.Cookie("session_id")
	if err != nil {
		return nil, err
	}
	return h.userService.Authenticate(r.Context(), cookie.Value)
}

func (h *Handler) signUp(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.maxBodyBytes)
	defer r.Body.Close()

	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	user, err := h.userService.SignUp(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrUserAlreadyExists) {
			response.Error(w, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, service.ErrInvalidInput) {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		slog.Error("signup failed", "error", err)
		response.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}

	response.JSON(w, http.StatusCreated, map[string]any{
		"id":    user.ID,
		"email": user.Email,
	})
}

func (h *Handler) logIn(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.maxBodyBytes)
	defer r.Body.Close()

	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	session, err := h.userService.LogIn(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			response.Error(w, http.StatusUnauthorized, err.Error())
			return
		}
		slog.Error("login failed", "error", err)
		response.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}

	isHTTPS := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    session.ID,
		Path:     "/",
		Expires:  session.ExpiresAt,
		HttpOnly: true,
		Secure:   isHTTPS,
		SameSite: http.SameSiteLaxMode,
	})

	response.JSON(w, http.StatusOK, map[string]string{"status": "authenticated"})
}

func (h *Handler) logOut(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_id")
	if err == nil {
		_ = h.userService.LogOut(r.Context(), cookie.Value)
	}

	isHTTPS := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isHTTPS,
		SameSite: http.SameSiteLaxMode,
	})

	response.JSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	user, err := h.getSessionUser(r)
	if err != nil {
		response.JSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}

	response.JSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"id":            user.ID,
		"email":         user.Email,
	})
}

func (h *Handler) userURLs(w http.ResponseWriter, r *http.Request) {
	user, err := h.getSessionUser(r)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	links, err := h.service.GetByUserID(r.Context(), user.ID)
	if err != nil {
		slog.Error("failed to get user urls", "error", err)
		response.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}

	res := make([]linkResponse, len(links))
	for i, l := range links {
		res[i] = h.toResponse(&l)
	}

	response.JSON(w, http.StatusOK, res)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	contentType := r.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(contentType), "application/json") {
		response.Error(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.maxBodyBytes)
	defer r.Body.Close()

	var request createRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		response.Error(w, http.StatusBadRequest, decodeError(err))
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		response.Error(w, http.StatusBadRequest, "request body must contain one JSON object")
		return
	}

	if request.ExpiresInSeconds < 0 {
		response.Error(w, http.StatusBadRequest, "expires_in_seconds cannot be negative")
		return
	}
	if request.ExpiresInSeconds > 9223372036 {
		response.Error(w, http.StatusBadRequest, "expires_in_seconds is too large")
		return
	}

	var userID *int64
	user, err := h.getSessionUser(r)
	if err == nil {
		userID = &user.ID
	}

	link, err := h.service.Create(r.Context(), service.CreateInput{
		OriginalURL: request.URL,
		CustomAlias: request.CustomAlias,
		ExpiresIn:   time.Duration(request.ExpiresInSeconds) * time.Second,
		UserID:      userID,
	})
	if err != nil {
		if errors.Is(err, repository.ErrConflict) && request.CustomAlias != "" {
			response.JSON(w, http.StatusConflict, conflictResponse{
				Error:    "short code already exists",
				ShortURL: h.baseURL + "/" + request.CustomAlias,
			})
			return
		}
		h.writeServiceError(w, err)
		return
	}

	response.JSON(w, http.StatusCreated, h.toResponse(link))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	link, err := h.service.Get(r.Context(), r.PathValue("code"))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, h.toResponse(link))
}

func (h *Handler) redirect(w http.ResponseWriter, r *http.Request) {
	link, err := h.service.Resolve(r.Context(), r.PathValue("code"))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	http.Redirect(w, r, link.OriginalURL, http.StatusFound)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	var userID *int64
	user, err := h.getSessionUser(r)
	if err == nil {
		userID = &user.ID
	}

	if err := h.service.Delete(r.Context(), r.PathValue("code"), userID); err != nil {
		h.writeServiceError(w, err)
		return
	}
	response.JSON(w, http.StatusNoContent, nil)
}

func (h *Handler) toResponse(link *model.Link) linkResponse {
	return linkResponse{
		ID:          link.ID,
		Code:        link.Code,
		ShortURL:    h.baseURL + "/" + link.Code,
		OriginalURL: link.OriginalURL,
		CreatedAt:   link.CreatedAt,
		ExpiresAt:   link.ExpiresAt,
		Visits:      link.Visits,
	}
}

func (h *Handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, repository.ErrConflict):
		response.Error(w, http.StatusConflict, "short code already exists")
	case errors.Is(err, repository.ErrNotFound), errors.Is(err, service.ErrExpired):
		response.Error(w, http.StatusNotFound, "short link not found")
	case errors.Is(err, service.ErrUnauthorizedLink):
		response.Error(w, http.StatusForbidden, err.Error())
	default:
		slog.Error("request failed", "error", err)
		response.Error(w, http.StatusInternalServerError, "internal server error")
	}
}

func decodeError(err error) string {
	var maxBytesError *http.MaxBytesError
	switch {
	case errors.As(err, &maxBytesError):
		return "request body is too large"
	case errors.Is(err, io.EOF):
		return "request body is required"
	default:
		return fmt.Sprintf("invalid JSON body: %v", err)
	}
}

const homePage = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>SnapLink &mdash; Premium URL Shortener</title>
  <link rel="icon" type="image/x-icon" href="/favicon.ico">
  <link rel="icon" type="image/png" sizes="16x16" href="/static/assets/favicon-16x16.png">
  <link rel="icon" type="image/png" sizes="32x32" href="/static/assets/favicon-32x32.png">
  <link rel="icon" type="image/png" sizes="48x48" href="/static/assets/favicon-48x48.png">
  <link rel="apple-touch-icon" sizes="180x180" href="/static/assets/favicon-180x180.png">
  <link rel="manifest" href="/static/assets/site.webmanifest">
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Outfit:wght@300;400;500;600;700;800&family=JetBrains+Mono:wght@400;500;600&display=swap" rel="stylesheet">
  <style>
    :root {
      --bg-primary: #030712;
      --card-bg: rgba(17, 24, 39, 0.75);
      --card-border: rgba(255, 255, 255, 0.08);
      --accent-purple: #a855f7;
      --accent-indigo: #6366f1;
      --accent-cyan: #06b6d4;
      --text-primary: #f8fafc;
      --text-secondary: #94a3b8;
      --text-muted: #64748b;
      --transition-smooth: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
      --toggle-bg: rgba(30, 41, 59, 0.8);
      --toggle-border: rgba(255,255,255,0.12);
    }

    :root.light {
      --bg-primary: #f1f5f9;
      --card-bg: rgba(255, 255, 255, 0.85);
      --card-border: rgba(0, 0, 0, 0.08);
      --text-primary: #0f172a;
      --text-secondary: #475569;
      --text-muted: #94a3b8;
      --toggle-bg: rgba(226, 232, 240, 0.9);
      --toggle-border: rgba(0,0,0,0.1);
    }

    :root.light body {
      background-image:
        radial-gradient(circle at 10% 20%, rgba(99, 102, 241, 0.07) 0%, transparent 45%),
        radial-gradient(circle at 90% 80%, rgba(168, 85, 247, 0.07) 0%, transparent 45%);
    }

    :root.light input {
      background: rgba(241, 245, 249, 0.8);
      border-color: rgba(0,0,0,0.1);
      color: var(--text-primary);
    }

    :root.light input:focus {
      background: #fff;
      border-color: var(--accent-indigo);
    }

    :root.light .glass-card {
      box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.12);
    }

    :root.light .pill-selector {
      background: rgba(226, 232, 240, 0.7);
      border-color: rgba(0,0,0,0.08);
    }

    :root.light .history-item {
      box-shadow: 0 4px 12px rgba(0,0,0,0.06);
    }

    :root.light .result-card {
      background: rgba(99, 102, 241, 0.04);
    }

    :root.light .result-body {
      background: rgba(241, 245, 249, 0.8);
      border-color: rgba(0,0,0,0.06);
    }

    :root.light .copy-btn {
      background: rgba(0,0,0,0.04);
      border-color: rgba(0,0,0,0.08);
      color: var(--text-primary);
    }

    :root.light .copy-btn:hover {
      background: rgba(0,0,0,0.09);
    }

    /* --- Theme Toggle --- */
    .theme-toggle {
      position: fixed;
      top: 18px;
      right: 22px;
      z-index: 1000;
      display: flex;
      align-items: center;
      gap: 8px;
      background: var(--toggle-bg);
      border: 1px solid var(--toggle-border);
      border-radius: 50px;
      padding: 7px 14px;
      cursor: pointer;
      backdrop-filter: blur(12px);
      -webkit-backdrop-filter: blur(12px);
      transition: var(--transition-smooth);
      box-shadow: 0 4px 20px rgba(0,0,0,0.2);
      user-select: none;
    }

    .theme-toggle:hover {
      box-shadow: 0 6px 24px rgba(99,102,241,0.25);
      border-color: rgba(99,102,241,0.3);
      transform: translateY(-1px);
    }

    .theme-toggle .icon-sun,
    .theme-toggle .icon-moon {
      width: 17px;
      height: 17px;
      transition: var(--transition-smooth);
    }

    .theme-toggle .icon-sun { color: #f59e0b; }
    .theme-toggle .icon-moon { color: #a78bfa; }

    .theme-toggle .toggle-track {
      width: 36px;
      height: 20px;
      background: linear-gradient(135deg, var(--accent-purple), var(--accent-indigo));
      border-radius: 50px;
      position: relative;
      transition: var(--transition-smooth);
      box-shadow: 0 0 8px rgba(99,102,241,0.4);
    }

    .theme-toggle .toggle-thumb {
      width: 14px;
      height: 14px;
      background: #ffffff;
      border-radius: 50%;
      position: absolute;
      top: 3px;
      left: 3px;
      transition: transform 0.3s cubic-bezier(0.34,1.56,0.64,1);
      box-shadow: 0 1px 4px rgba(0,0,0,0.3);
    }

    :root.light .toggle-thumb {
      transform: translateX(16px);
    }

    :root.light .toggle-track {
      background: linear-gradient(135deg, #f59e0b, #fbbf24);
      box-shadow: 0 0 8px rgba(245,158,11,0.35);
    }

    * {
      box-sizing: border-box;
      margin: 0;
      padding: 0;
    }

    body {
      font-family: 'Outfit', sans-serif;
      background-color: var(--bg-primary);
      background-image: 
        radial-gradient(circle at 10% 20%, rgba(99, 102, 241, 0.12) 0%, transparent 45%),
        radial-gradient(circle at 90% 80%, rgba(168, 85, 247, 0.12) 0%, transparent 45%);
      color: var(--text-primary);
      min-height: 100vh;
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: flex-start;
      padding: 60px 20px;
      overflow-y: auto;
    }

    main {
      width: 100%;
      max-width: 600px;
      margin-bottom: 24px;
    }

    header {
      text-align: center;
      margin-bottom: 36px;
    }

    header h1 {
      font-size: 3rem;
      font-weight: 800;
      letter-spacing: -0.05em;
      background: linear-gradient(135deg, var(--accent-purple), var(--accent-indigo), var(--accent-cyan));
      -webkit-background-clip: text;
      -webkit-text-fill-color: transparent;
      margin-bottom: 12px;
      display: flex;
      align-items: center;
      justify-content: center;
      gap: 10px;
    }

    header p {
      color: var(--text-secondary);
      font-size: 1.15rem;
      font-weight: 400;
    }

    .glass-card {
      background: var(--card-bg);
      backdrop-filter: blur(16px);
      -webkit-backdrop-filter: blur(16px);
      border: 1px solid var(--card-border);
      border-radius: 24px;
      padding: 36px;
      box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.5);
    }

    .user-status-card {
      background: var(--card-bg);
      backdrop-filter: blur(16px);
      -webkit-backdrop-filter: blur(16px);
      border: 1px solid var(--card-border);
      border-radius: 16px;
      margin-bottom: 20px;
      padding: 14px 24px;
      display: flex;
      justify-content: space-between;
      align-items: center;
      font-size: 0.9rem;
      width: 100%;
      max-width: 600px;
    }

    .user-status-card a {
      color: var(--accent-indigo);
      text-decoration: none;
      font-weight: 600;
    }

    .user-status-card a:hover {
      text-decoration: underline;
    }

    .auth-card {
      display: none;
      width: 100%;
      max-width: 600px;
      margin-bottom: 24px;
    }

    .auth-card h2 {
      margin-bottom: 20px;
      font-size: 1.5rem;
      text-align: center;
    }

    .form-group {
      margin-bottom: 22px;
      display: flex;
      flex-direction: column;
      gap: 8px;
    }

    label {
      font-size: 0.8rem;
      font-weight: 600;
      color: var(--text-secondary);
      letter-spacing: 0.05em;
      text-transform: uppercase;
      display: flex;
      align-items: center;
      gap: 6px;
    }

    .input-wrapper {
      position: relative;
      display: flex;
      align-items: center;
    }

    .input-wrapper svg {
      position: absolute;
      left: 16px;
      width: 18px;
      height: 18px;
      color: var(--text-muted);
      pointer-events: none;
    }

    input {
      width: 100%;
      padding: 14px 16px 14px 46px;
      background: rgba(17, 24, 39, 0.6);
      border: 1px solid rgba(255, 255, 255, 0.08);
      border-radius: 14px;
      color: var(--text-primary);
      font-size: 1rem;
      font-family: inherit;
      transition: var(--transition-smooth);
    }

    input:focus {
      outline: none;
      border-color: var(--accent-indigo);
      box-shadow: 0 0 0 3px rgba(99, 102, 241, 0.25);
      background: rgba(17, 24, 39, 0.8);
    }

   .split-row {
    display: grid;
    grid-template-columns: 1fr 1.35fr;
    gap: 16px;
}

    @media (max-width: 520px) {
      .split-row {
        grid-template-columns: 1fr;
      }
    }

   .pill-selector {
    display: flex;
    background: rgba(17, 24, 39, 0.6);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 14px;
    padding: 4px;
    width: 100%;
    gap: 2px;
}

.pill-option {
    flex: 1;
    text-align: center;
    padding: 10px 4px;
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--text-secondary);
    border-radius: 10px;
    cursor: pointer;
    transition: var(--transition-smooth);
    user-select: none;
    white-space: nowrap;
}
    .pill-option:hover {
      color: var(--text-primary);
      background: rgba(255, 255, 255, 0.04);
    }

    .pill-option.active {
      color: #ffffff;
      background: linear-gradient(135deg, var(--accent-purple), var(--accent-indigo));
      box-shadow: 0 4px 15px rgba(99, 102, 241, 0.3);
    }

    button.btn-primary {
      width: 100%;
      padding: 16px;
      margin-top: 12px;
      background: linear-gradient(135deg, var(--accent-purple), var(--accent-indigo));
      border: none;
      border-radius: 14px;
      color: #ffffff;
      font-size: 1.1rem;
      font-weight: 700;
      cursor: pointer;
      transition: var(--transition-smooth);
      box-shadow: 0 8px 20px rgba(99, 102, 241, 0.2);
      display: flex;
      align-items: center;
      justify-content: center;
      gap: 8px;
    }

    button.btn-primary:hover {
      transform: translateY(-2px);
      box-shadow: 0 15px 30px rgba(99, 102, 241, 0.35);
      background: linear-gradient(135deg, #b866ff, #7376ff);
    }

    button.btn-primary:active {
      transform: translateY(0);
    }

    .result-card {
      display: none;
      margin-top: 28px;
      background: rgba(99, 102, 241, 0.06);
      border: 1px dashed rgba(99, 102, 241, 0.3);
      border-radius: 16px;
      padding: 24px;
      animation: fadeIn 0.4s cubic-bezier(0.16, 1, 0.3, 1) forwards;
    }

    .result-card h3 {
      font-size: 1rem;
      font-weight: 700;
      margin-bottom: 14px;
      color: var(--accent-cyan);
      display: flex;
      align-items: center;
      gap: 6px;
    }

    .result-body {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 16px;
      background: rgba(3, 7, 18, 0.6);
      padding: 14px;
      border-radius: 10px;
      border: 1px solid rgba(255, 255, 255, 0.05);
    }

    .result-link {
      font-family: 'JetBrains Mono', monospace;
      color: var(--text-primary);
      font-size: 1rem;
      text-decoration: none;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      flex: 1;
    }

    .result-link:hover {
      color: var(--accent-cyan);
      text-decoration: underline;
    }

    .copy-btn {
      background: rgba(255, 255, 255, 0.05);
      border: 1px solid rgba(255, 255, 255, 0.08);
      padding: 8px 14px;
      border-radius: 8px;
      color: var(--text-primary);
      cursor: pointer;
      font-size: 0.8rem;
      font-weight: 600;
      display: flex;
      align-items: center;
      gap: 6px;
      transition: var(--transition-smooth);
      white-space: nowrap;
    }

    .copy-btn:hover {
      background: rgba(255, 255, 255, 0.12);
    }

    .copy-btn.copied {
      background: rgba(16, 185, 129, 0.15);
      border-color: rgba(16, 185, 129, 0.3);
      color: #10b981;
    }

    .history-section {
      width: 100%;
      max-width: 600px;
      margin-top: 36px;
      display: none;
    }

    .history-section h2 {
      font-size: 1.2rem;
      font-weight: 700;
      color: var(--text-secondary);
      display: flex;
      align-items: center;
      gap: 8px;
    }

    .history-list {
      display: flex;
      flex-direction: column;
      gap: 14px;
    }

    .history-item {
      background: var(--card-bg);
      border: 1px solid var(--card-border);
      border-radius: 16px;
      padding: 20px;
      display: flex;
      flex-direction: column;
      gap: 12px;
      transition: var(--transition-smooth);
      animation: slideIn 0.4s cubic-bezier(0.16, 1, 0.3, 1) forwards;
    }

    .history-item:hover {
      border-color: rgba(99, 102, 241, 0.25);
      transform: translateY(-2px);
      box-shadow: 0 10px 20px -10px rgba(0, 0, 0, 0.3);
    }

    .history-header {
      display: flex;
      justify-content: space-between;
      align-items: flex-start;
      gap: 16px;
    }

    .history-urls {
      flex: 1;
      overflow: hidden;
    }

    .short-url-row {
      display: flex;
      align-items: center;
      gap: 8px;
      margin-bottom: 6px;
    }

    .short-url-row a {
      font-family: 'JetBrains Mono', monospace;
      color: var(--accent-cyan);
      text-decoration: none;
      font-weight: 600;
      font-size: 1.05rem;
    }

    .short-url-row a:hover {
      text-decoration: underline;
    }

    .original-url {
      font-size: 0.85rem;
      color: var(--text-secondary);
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .history-actions {
      display: flex;
      gap: 8px;
      align-items: center;
    }

    .history-footer {
      display: flex;
      align-items: center;
      justify-content: space-between;
      font-size: 0.8rem;
      color: var(--text-muted);
      border-top: 1px solid rgba(255, 255, 255, 0.05);
      padding-top: 12px;
    }

    .footer-badges {
      display: flex;
      gap: 8px;
    }

    .badge {
      padding: 3px 8px;
      border-radius: 6px;
      font-size: 0.75rem;
      font-weight: 600;
    }

    .badge-visits {
      background: rgba(99, 102, 241, 0.12);
      color: #818cf8;
      border: 1px solid rgba(99, 102, 241, 0.15);
    }

    .badge-active {
      background: rgba(16, 185, 129, 0.1);
      color: #34d399;
      border: 1px solid rgba(16, 185, 129, 0.15);
    }

    .badge-expired {
      background: rgba(239, 68, 68, 0.1);
      color: #f87171;
      border: 1px solid rgba(239, 68, 68, 0.15);
    }

    .delete-btn {
      background: transparent;
      border: none;
      color: var(--text-muted);
      cursor: pointer;
      padding: 6px;
      border-radius: 8px;
      transition: var(--transition-smooth);
      display: flex;
      align-items: center;
      justify-content: center;
    }

    .delete-btn:hover {
      color: #f87171;
      background: rgba(239, 68, 68, 0.08);
    }

    @keyframes fadeIn {
      from { opacity: 0; transform: translateY(8px); }
      to { opacity: 1; transform: translateY(0); }
    }

    @keyframes slideIn {
      from { opacity: 0; transform: translateY(12px); }
      to { opacity: 1; transform: translateY(0); }
    }
  </style>
</head>
<body>
  <!-- Theme Toggle -->
  <button id="theme-toggle" class="theme-toggle" aria-label="Toggle light/dark mode" title="Toggle light/dark mode">
    <svg class="icon-moon" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor">
      <path stroke-linecap="round" stroke-linejoin="round" d="M21.752 15.002A9.718 9.718 0 0118 15.75c-5.385 0-9.75-4.365-9.75-9.75 0-1.33.266-2.597.748-3.752A9.753 9.753 0 003 11.25C3 16.635 7.365 21 12.75 21a9.753 9.753 0 009.002-5.998z" />
    </svg>
    <div class="toggle-track">
      <div class="toggle-thumb"></div>
    </div>
    <svg class="icon-sun" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor">
      <path stroke-linecap="round" stroke-linejoin="round" d="M12 3v2.25m6.364.386l-1.591 1.591M21 12h-2.25m-.386 6.364l-1.591-1.591M12 18.75V21m-4.773-4.227l-1.591 1.591M5.25 12H3m4.227-4.773L5.636 5.636M15.75 12a3.75 3.75 0 11-7.5 0 3.75 3.75 0 017.5 0z" />
    </svg>
  </button>

  <div class="user-status-card" id="user-status-card">
    <div id="user-info">Loading connection status...</div>
    <div id="user-action-container"></div>
  </div>

  <main>
    <header>
      <h1>
        <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2.5" stroke="currentColor" style="width:34px;height:34px;color:#a855f7;">
          <path stroke-linecap="round" stroke-linejoin="round" d="M13.19 8.688a4.5 4.5 0 011.242 7.244l-4.5 4.5a4.5 4.5 0 01-6.364-6.364l1.757-1.757m13.35-.622l1.757-1.757a4.5 4.5 0 00-6.364-6.364l-4.5 4.5a4.5 4.5 0 006.364 6.364" />
        </svg>
        SnapLink
      </h1>
      <p>Shorten, manage, and track your links with ease.</p>
    </header>

    <div class="glass-card auth-card" id="auth-card">
      <h2 id="auth-title">Welcome Back</h2>
      <form id="auth-form">
        <div class="form-group">
          <label for="auth-email">Email Address</label>
          <input type="email" id="auth-email" placeholder="name@domain.com" required style="padding-left:16px;">
        </div>
        <div class="form-group">
          <label for="auth-password">Password</label>
          <input type="password" id="auth-password" placeholder="••••••••" required style="padding-left:16px;">
        </div>
        <button type="submit" class="btn-primary" style="margin-top:10px;" id="auth-submit-btn">Login</button>
      </form>
      <div style="text-align:center; margin-top:18px; font-size:0.85rem; color:var(--text-secondary);">
        <span id="auth-toggle-text">Don't have an account?</span>
        <a href="#" onclick="toggleAuthMode()" id="auth-toggle-link" style="color:var(--accent-indigo); text-decoration:none; font-weight:600; margin-left:4px;">Sign Up</a>
      </div>
      <div style="text-align:center; margin-top:12px;">
        <a href="#" onclick="cancelAuth()" style="color:var(--text-muted); text-decoration:none; font-size:0.85rem;">Cancel / Guest Mode</a>
      </div>
    </div>

    <div class="glass-card" id="shortener-card">
      <form id="shorten-form">
        <div class="form-group">
          <label for="url">Destination URL</label>
          <div class="input-wrapper">
            <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" d="M12 21a9.004 9.004 0 008.716-6.747M12 21a9.004 9.004 0 01-8.716-6.747M12 21c2.485 0 4.5-4.03 4.5-9S14.485 3 12 3m0 18c-2.485 0-4.5-4.03-4.5-9S9.515 3 12 3m0 0a8.997 8.997 0 017.843 4.582M12 3a8.997 8.997 0 00-7.843 4.582m15.686 0A11.953 11.953 0 0112 10.5c-2.998 0-5.74-1.1-7.843-2.918m15.686 0A8.959 8.959 0 0121 12c0 .778-.099 1.533-.284 2.253m0 0A17.919 17.919 0 0112 16.5c-3.162 0-6.133-.815-8.716-2.247m0 0A9.015 9.015 0 013 12c0-1.605.42-3.113 1.157-4.418" />
            </svg>
            <input id="url" type="url" placeholder="https://example.com/some/long/destination/path" required>
          </div>
        </div>

        <div class="split-row">
          <div class="form-group">
            <label for="alias">
              Custom Alias <span style="text-transform:none;color:var(--text-muted);font-weight:400;margin-left:4px;">(optional)</span>
            </label>
            <div class="input-wrapper">
              <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" d="M9.528 17.116A1.5 1.5 0 1111 15.5h.01a1.5 1.5 0 111.472 1.616l-1.472.01-.01-1.626zm0 0l-.01 2.25m3.73-3.73l-.01-2.25m-3.72-3.72a4.5 4.5 0 117.47 0" />
              </svg>
              <input id="alias" pattern="[A-Za-z0-9_-]{4,32}" placeholder="e.g. my-shortcut">
            </div>
          </div>

          <div class="form-group">
            <label>Link Expiry</label>
            <div class="pill-selector">
             <div class="pill-option active" data-value="0">Never</div>
<div class="pill-option" data-value="3600">1 Hour</div>
<div class="pill-option" data-value="86400">1 Day</div>
<div class="pill-option" data-value="604800">7 Days</div>
            </div>
          </div>
        </div>

        <button type="submit" class="btn-primary">
          <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2.5" stroke="currentColor" style="width:18px;height:18px;">
            <path stroke-linecap="round" stroke-linejoin="round" d="M9.813 15.904L9 21l8.904-4.452L21 9l-4.5-4.5L9.813 15.904z" stroke-linejoin="round" />
            <path stroke-linecap="round" stroke-linejoin="round" d="M9 21l3-3m-3 3l-3-3" />
          </svg>
          Shorten URL
        </button>
      </form>

      <div id="result" class="result-card"></div>
    </div>
  </main>

  <section id="history-section" class="history-section">
    <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:18px; flex-wrap:wrap; gap:12px;">
      <h2>
        <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2.2" stroke="currentColor" style="width:20px;height:20px;color:var(--accent-indigo);">
          <path stroke-linecap="round" stroke-linejoin="round" d="M3.75 6A2.25 2.25 0 016 3.75h2.25A2.25 2.25 0 0110.5 6v2.25a2.25 2.25 0 01-2.25 2.25H6a2.25 2.25 0 01-2.25-2.25V6zM3.75 15.75A2.25 2.25 0 016 13.5h2.25a2.25 2.25 0 012.25 2.25V18a2.25 2.25 0 01-2.25 2.25H6A2.25 2.25 0 013.75 18v-2.25zM13.5 6a2.25 2.25 0 012.25-2.25H18A2.25 2.25 0 0120.25 6v2.25A2.25 2.25 0 0118 10.5h-2.25a2.25 2.25 0 01-2.25-2.25V6zM13.5 15.75a2.25 2.25 0 012.25-2.25H18a2.25 2.25 0 012.25 2.25V18A2.25 2.25 0 0118 20.25h-2.25A2.25 2.25 0 0113.5 18v-2.25z" />
        </svg>
        <span id="history-title">Your Recent Links</span>
      </h2>
      <input type="text" id="search-input" placeholder="Search links..." style="max-width:200px; padding:8px 12px; font-size:0.85rem; border-radius:8px; border:1px solid var(--card-border); background:rgba(17,24,39,0.4); color:var(--text-primary);">
    </div>
    <div id="history-list" class="history-list"></div>
  </section>

  <script>
    var isSignupMode = false;
    var currentUser = null;
    var serverHistory = [];

    document.addEventListener("DOMContentLoaded", function() {
      // --- Theme Toggle Logic ---
      var root = document.documentElement;
      var toggleBtn = document.getElementById("theme-toggle");
      var savedTheme = localStorage.getItem("snaplink_theme") || "dark";
      if (savedTheme === "light") root.classList.add("light");

      toggleBtn.addEventListener("click", function() {
        var isLight = root.classList.toggle("light");
        localStorage.setItem("snaplink_theme", isLight ? "light" : "dark");
      });

      // --- Expire Selector Logic ---
      var pillOptions = document.querySelectorAll(".pill-option");
      var selectedExpiresIn = 0;

      pillOptions.forEach(function(pill) {
        pill.addEventListener("click", function() {
          pillOptions.forEach(function(p) { p.classList.remove("active"); });
          pill.classList.add("active");
          selectedExpiresIn = parseInt(pill.dataset.value);
        });
      });

      // --- Setup Auth Forms & Listeners ---
      checkAuthStatus().then(function() {
        setupFormListeners();
      });

      // Search input real-time filtering
      document.getElementById("search-input").addEventListener("input", function(e) {
        renderHistoryList(e.target.value);
      });
    });

    function checkAuthStatus() {
      return fetch("/api/v1/me")
        .then(function(res) { return res.json(); })
        .then(function(data) {
          if (data.authenticated) {
            currentUser = { email: data.email, id: data.id };
            updateUserBar(true);
            return loadServerHistory();
          } else {
            currentUser = null;
            updateUserBar(false);
            renderHistoryList();
          }
        })
        .catch(function(err) {
          console.error("Auth status error:", err);
          renderHistoryList();
        });
    }

    function updateUserBar(loggedIn) {
      var info = document.getElementById("user-info");
      var action = document.getElementById("user-action-container");

      if (loggedIn && currentUser) {
        info.innerHTML = "Logged in as <strong>" + currentUser.email + "</strong>";
        action.innerHTML = "<a href='#' onclick='handleLogout()'>Logout</a>";
      } else {
        info.innerHTML = "Using as <em>Guest</em>";
        action.innerHTML = "<a href='#' onclick='showAuthCard()'>Login / Sign Up</a> to save links permanently";
      }
    }

    function showAuthCard() {
      document.getElementById("shortener-card").style.display = "none";
      document.getElementById("auth-card").style.display = "block";
    }

    function cancelAuth() {
      document.getElementById("auth-card").style.display = "none";
      document.getElementById("shortener-card").style.display = "block";
    }

    function showAuthView(signup) {
      isSignupMode = signup;
      var title = document.getElementById("auth-title");
      var btn = document.getElementById("auth-submit-btn");
      var txt = document.getElementById("auth-toggle-text");
      var link = document.getElementById("auth-toggle-link");

      if (isSignupMode) {
        title.textContent = "Create Account";
        btn.textContent = "Register";
        txt.textContent = "Already have an account?";
        link.textContent = "Log In";
      } else {
        title.textContent = "Welcome Back";
        btn.textContent = "Log In";
        txt.textContent = "Don't have an account?";
        link.textContent = "Sign Up";
      }
    }

    function toggleAuthMode() {
      showAuthView(!isSignupMode);
    }

    function handleLogout() {
      fetch("/api/v1/logout", { method: "POST" })
        .then(function() {
          currentUser = null;
          updateUserBar(false);
          renderHistoryList();
        })
        .catch(function(err) {
          console.error("Logout failed:", err);
        });
    }

    function setupFormListeners() {
      var authForm = document.getElementById("auth-form");
      authForm.addEventListener("submit", function(e) {
        e.preventDefault();
        var email = document.getElementById("auth-email").value;
        var password = document.getElementById("auth-password").value;
        var url = isSignupMode ? "/api/v1/signup" : "/api/v1/login";

        fetch(url, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ email: email, password: password })
        })
        .then(function(res) {
          return res.json().then(function(data) {
            return { ok: res.ok, data: data };
          });
        })
        .then(function(result) {
          if (!result.ok) {
            alert(result.data.error || "Authentication failed");
            return;
          }

          if (isSignupMode) {
            isSignupMode = false;
            fetch("/api/v1/login", {
              method: "POST",
              headers: { "Content-Type": "application/json" },
              body: JSON.stringify({ email: email, password: password })
            })
            .then(function(loginRes) {
              if (loginRes.ok) {
                checkAuthStatus().then(cancelAuth);
              } else {
                showAuthView(false);
              }
            });
          } else {
            checkAuthStatus().then(cancelAuth);
          }
        })
        .catch(function(err) {
          console.error("Auth error:", err);
          alert("Network error occurred.");
        });
      });

      // Shorten Form
      var form = document.querySelector("#shorten-form");
      var resultCard = document.querySelector("#result");
      var pillOptions = document.querySelectorAll(".pill-option");

      form.addEventListener("submit", function(event) {
        event.preventDefault();
        resultCard.style.display = "block";
        resultCard.innerHTML = '<h3 style="color:var(--text-secondary)">Shortening your link...</h3>';

        var payload = { url: document.querySelector("#url").value };
        var alias = document.querySelector("#alias").value.trim();
        if (alias) payload.custom_alias = alias;

        var selectedExpiresIn = 0;
        document.querySelectorAll(".pill-option").forEach(function(p) {
          if (p.classList.contains("active")) {
            selectedExpiresIn = parseInt(p.dataset.value);
          }
        });
        if (selectedExpiresIn > 0) payload.expires_in_seconds = selectedExpiresIn;

        fetch("/api/v1/urls", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(payload)
        })
        .then(function(res) {
          return res.json().then(function(data) {
            return { ok: res.ok, data: data };
          });
        })
        .then(function(result) {
          if (!result.ok) {
            var errMsg = result.data.error || "Unable to create link";
            resultCard.innerHTML = '<div style="color: #fca5a5; font-weight:600; margin-bottom:8px;">Error: ' + errMsg + '</div>';
            if (result.data.short_url) {
              resultCard.innerHTML += 
                '<div class="result-body">\n' +
                '  <span style="font-size:0.85rem;color:var(--text-secondary)">Existing URL:</span>\n' +
                '  <a class="result-link" href="' + result.data.short_url + '" target="_blank" rel="noopener">' + result.data.short_url + '</a>\n' +
                '  <button id="err-copy-btn" class="copy-btn" onclick="copyText(\'' + result.data.short_url + '\', \'err-copy-btn\')">\n' +
                '    <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" style="width:14px;height:14px;">\n' +
                '      <path stroke-linecap="round" stroke-linejoin="round" d="M15.75 17.25v3.375c0 .621-.504 1.125-1.125 1.125h-9.75a1.125 1.125 0 01-1.125-1.125V7.875c0-.621.504-1.125 1.125-1.125H5.25m9.9 9.9l3.89-3.89a.75.75 0 000-1.06l-3.89-3.89m3.89 3.89H11.25M9 11.25h.008v.008H9v-.008z" />\n' +
                '    </svg> Copy\n' +
                '  </button>\n' +
                '</div>';
            }
            return;
          }

          var data = result.data;
          resultCard.innerHTML = 
            '<h3>Link Created Successfully!</h3>\n' +
            '<div class="result-body">\n' +
            '  <a id="result-link" class="result-link" href="' + data.short_url + '" target="_blank" rel="noopener">' + data.short_url + '</a>\n' +
            '  <button id="main-copy-btn" class="copy-btn" onclick="copyText(\'' + data.short_url + '\', \'main-copy-btn\')">\n' +
            '    <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" style="width:14px;height:14px;">\n' +
            '      <path stroke-linecap="round" stroke-linejoin="round" d="M15.75 17.25v3.375c0 .621-.504 1.125-1.125 1.125h-9.75a1.125 1.125 0 01-1.125-1.125V7.875c0-.621.504-1.125 1.125-1.125H5.25m9.9 9.9l3.89-3.89a.75.75 0 000-1.06l-3.89-3.89m3.89 3.89H11.25M9 11.25h.008v.008H9v-.008z" />\n' +
            '    </svg> Copy Link\n' +
            '  </button>\n' +
            '</div>';

          if (currentUser) {
            loadServerHistory();
          } else {
            addToLocalHistory(data);
          }

          form.reset();
          pillOptions.forEach(function(p) { p.classList.remove("active"); });
          var defPill = document.querySelector('[data-value="0"]');
          if (defPill) defPill.classList.add("active");
        })
        .catch(function(err) {
          console.error("Shorten error:", err);
          resultCard.innerHTML = '<div style="color:#fca5a5; font-weight:600;">Server error.</div>';
        });
      });
    }

    function loadServerHistory() {
      return fetch("/api/v1/user/urls")
        .then(function(res) {
          if (res.ok) {
            return res.json().then(function(data) {
              serverHistory = data;
              renderHistoryList();
            });
          }
        })
        .catch(function(err) {
          console.error("Server history error:", err);
        });
    }

    function getLocalHistory() {
      return JSON.parse(localStorage.getItem("snaplink_history") || "[]");
    }

    function saveLocalHistory(history) {
      localStorage.setItem("snaplink_history", JSON.stringify(history));
    }

    function addToLocalHistory(link) {
      var history = getLocalHistory();
      history = history.filter(function(item) { return item.code !== link.code; });
      history.unshift({
        code: link.code,
        short_url: link.short_url,
        original_url: link.original_url,
        created_at: link.created_at,
        expires_at: link.expires_at,
        visits: link.visits || 0
      });
      if (history.length > 15) history.pop();
      saveLocalHistory(history);
      renderHistoryList();
    }

    function deleteLocalHistoryItem(code) {
      var history = getLocalHistory();
      history = history.filter(function(item) { return item.code !== code; });
      saveLocalHistory(history);
      renderHistoryList();
    }

    function deleteServerHistoryItem(code) {
      if (!confirm("Are you sure you want to delete short code: " + code + "?")) {
        return;
      }
      fetch("/api/v1/urls/" + code, { method: "DELETE" })
        .then(function(res) {
          if (res.ok) {
            loadServerHistory();
          } else {
            alert("Failed to delete link.");
          }
        })
        .catch(function(err) {
          console.error("Delete err:", err);
        });
    }

    function renderHistoryList(query) {
      var q = (query || "").toLowerCase();
      var historySection = document.getElementById("history-section");
      var historyList = document.getElementById("history-list");
      var title = document.getElementById("history-title");

      var list = currentUser ? serverHistory : getLocalHistory();

      if (currentUser) {
        title.textContent = "Your Managed Links";
      } else {
        title.textContent = "Recent Guest Links";
      }

      var filtered = list.filter(function(item) {
        return item.code.toLowerCase().indexOf(q) !== -1 || item.original_url.toLowerCase().indexOf(q) !== -1;
      });

      if (filtered.length === 0) {
        if (query) {
          historyList.innerHTML = "<div style='text-align:center; padding:20px; color:var(--text-muted);'>No matching links found.</div>";
          historySection.style.display = "block";
        } else {
          historySection.style.display = "none";
        }
        return;
      }

      historySection.style.display = "block";
      historyList.innerHTML = "";

      filtered.forEach(function(item, index) {
        var isExpired = item.expires_at && new Date(item.expires_at) < new Date();
        var statusText = isExpired ? "Expired" : formatRelativeTime(item.expires_at);
        var badgeClass = isExpired ? "badge-expired" : "badge-active";
        var btnId = "history-copy-" + index;

        var card = document.createElement("div");
        card.className = "history-item";
        card.innerHTML = '\n' +
          '            <div class="history-header">\n' +
          '              <div class="history-urls">\n' +
          '                <div class="short-url-row">\n' +
          '                  <a href="' + item.short_url + '" target="_blank" rel="noopener">' + item.short_url + '</a>\n' +
          '                </div>\n' +
          '                <div class="original-url" title="' + item.original_url + '">' + item.original_url + '</div>\n' +
          '              </div>\n' +
          '              <div class="history-actions">\n' +
          '                <button id="' + btnId + '" class="copy-btn" onclick="copyText(\'' + item.short_url + '\', \'' + btnId + '\')">\n' +
          '                  <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" style="width:14px;height:14px;">\n' +
          '                    <path stroke-linecap="round" stroke-linejoin="round" d="M15.75 17.25v3.375c0 .621-.504 1.125-1.125 1.125h-9.75a1.125 1.125 0 01-1.125-1.125V7.875c0-.621.504-1.125 1.125-1.125H5.25m9.9 9.9l3.89-3.89a.75.75 0 000-1.06l-3.89-3.89m3.89 3.89H11.25M9 11.25h.008v.008H9v-.008z" />\n' +
          '                  </svg> Copy\n' +
          '                </button>\n' +
          '                <button class="delete-btn" title="Remove from History" data-code="' + item.code + '">\n' +
          '                  <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" style="width:16px;height:16px;">\n' +
          '                    <path stroke-linecap="round" stroke-linejoin="round" d="M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0" />\n' +
          '                  </svg>\n' +
          '                </button>\n' +
          '              </div>\n' +
          '            </div>\n' +
          '            <div class="history-footer">\n' +
          '              <span>Created: ' + new Date(item.created_at).toLocaleDateString() + '</span>\n' +
          '              <div class="footer-badges">\n' +
          '                <span class="badge ' + badgeClass + '">' + (isExpired ? "Expired" : "Expires: " + statusText) + '</span>\n' +
          '                <span class="badge badge-visits">' + item.visits + ' visits</span>\n' +
          '              </div>\n' +
          '            </div>\n';

        historyList.appendChild(card);
      });

      document.querySelectorAll(".delete-btn").forEach(function(btn) {
        btn.addEventListener("click", function() {
          if (currentUser) {
            deleteServerHistoryItem(btn.dataset.code);
          } else {
            deleteLocalHistoryItem(btn.dataset.code);
          }
        });
      });
    }

    window.copyText = function(text, elementId) {
      navigator.clipboard.writeText(text).then(function() {
        var btn = document.getElementById(elementId);
        if (!btn) return;
        var originalHTML = btn.innerHTML;
        btn.classList.add("copied");
        btn.innerHTML = '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2.5" stroke="currentColor" style="width:14px;height:14px;"><path stroke-linecap="round" stroke-linejoin="round" d="M4.5 12.75l6 6 9-13.5" /></svg> Copied!';
        setTimeout(function() {
          btn.classList.remove("copied");
          btn.innerHTML = originalHTML;
        }, 2000);
      });
    };

    function formatRelativeTime(dateStr) {
      if (!dateStr) return "Never";
      var date = new Date(dateStr);
      var now = new Date();
      if (date < now) return "Expired";
      
      var diffMs = date - now;
      var diffMins = Math.round(diffMs / 60000);
      var diffHours = Math.round(diffMs / 3600000);
      var diffDays = Math.round(diffMs / 86400000);

      if (diffMins < 60) return "in " + diffMins + "m";
      if (diffHours < 24) return "in " + diffHours + "h";
      return "in " + diffDays + "d";
    }
  </script>
</body>
</html>`
