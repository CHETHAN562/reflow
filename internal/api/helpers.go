package api

import (
	"encoding/json"
	"net/http"
	"reflow/internal/util"
)

// writeJSON encodes data to JSON and writes it to the response writer.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		err := json.NewEncoder(w).Encode(data)
		if err != nil {
			// Log the encoding error, but the header is already sent.
			util.Log.Errorf("Failed to encode JSON response: %v", err)
		}
	}
}

// writeError formats an error message as JSON and writes it with the given status code.
func writeError(w http.ResponseWriter, status int, message string, details ...string) {
	errorResponse := map[string]interface{}{
		"error": message,
	}
	if len(details) > 0 {
		errorResponse["details"] = details[0]
	}
	util.Log.Warnf("API Error %d: %s %v", status, message, details)
	writeJSON(w, status, errorResponse)
}
