package handlers

import (
	"encoding/json"
	"net/http"

	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"
)

func writeJSON(wtr http.ResponseWriter, v any) {
	if err := json.NewEncoder(wtr).Encode(v); err != nil {
		logger.Error("failed to encode response body", err)
	}
}
