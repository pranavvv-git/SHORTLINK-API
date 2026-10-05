package response

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type ErrorBody struct {
	Error string `json:"error"`
}

func JSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if status == http.StatusNoContent {
		return
	}
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("encode HTTP response", "error", err)
	}
}

func Error(w http.ResponseWriter, status int, message string) {
	JSON(w, status, ErrorBody{Error: message})
}
