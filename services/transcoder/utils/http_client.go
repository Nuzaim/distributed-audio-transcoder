package utils

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"time"
)

type AudioJobUpdate struct {
	OutputPath string `json:"output_path"`
}

func UpdateAudioJob(id string, data *AudioJobUpdate, logger *slog.Logger){
	jsonBuffer := new(bytes.Buffer)
	err := json.NewEncoder(jsonBuffer).Encode(data)
	if err != nil {
		logger.Error("Encoding error", "err", err)
		return
	}
	apiURL, err := url.Parse(os.Getenv("WEB_API_URL"))
	if err != nil {
		logger.Error("API URL parsing failed", "err", err)
		return
	}
	reqURL := apiURL.JoinPath("audio", id)
	req, err := http.NewRequest(http.MethodPatch, reqURL.String(), jsonBuffer)
	if err != nil {
		logger.Error("Request creation error", "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	// TODO: add context to dynamically change timeout.
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.Error("Request failed", "err", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
    logger.Error("Server rejected PATCH request", "status_code", resp.StatusCode)
    return
	}

	logger.Info("PATCH successful", "status_code", resp.StatusCode, "response_status", resp.Status)
}
