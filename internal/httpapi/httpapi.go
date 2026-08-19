package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
)

func BearerMatches(header, secret string) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	provided := strings.TrimPrefix(header, prefix)
	return len(provided) == len(secret) && subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) == 1
}

func DecodeJSON(w http.ResponseWriter, r *http.Request, destination any, invalidMessage string) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		WriteError(w, http.StatusUnsupportedMediaType, "content_type", "Content-Type must be application/json")
		return false
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_json", invalidMessage)
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		WriteError(w, http.StatusBadRequest, "invalid_json", invalidMessage)
		return false
	}
	return true
}

func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
