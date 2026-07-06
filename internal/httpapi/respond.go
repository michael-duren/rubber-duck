package httpapi

import (
	"encoding/json"
	"net/http"
)

// Detail is one line-level problem in a validation error response.
type Detail struct {
	Line    int    `json:"line,omitempty"`
	Message string `json:"message"`
}

type errorBody struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Details []Detail `json:"details,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string, details []Detail) {
	writeJSON(w, status, map[string]errorBody{"error": {Code: code, Message: message, Details: details}})
}
