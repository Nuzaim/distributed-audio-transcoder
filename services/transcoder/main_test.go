package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

func TestWrapperForCreateHlsStreamWithRealAudio(t *testing.T) {
	// Locate test audio file
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	sampleAudio := filepath.Join(repoRoot, "test_data", "sample_5s.mp3")
	if _, err := os.Stat(sampleAudio); err != nil {
		t.Skipf("sample audio not found at %s: %v", sampleAudio, err)
	}

	workDir := t.TempDir()
	jobID := "test-job-12345"
	jobInputAudioDir := filepath.Join(workDir, jobID)
	if err := os.MkdirAll(jobInputAudioDir, 0755); err != nil {
		t.Fatalf("create job input dir: %v", err)
	}
	localInput := filepath.Join(jobInputAudioDir, "audio.mp3")
	inputData, err := os.ReadFile(sampleAudio)
	if err != nil {
		t.Fatalf("read sample audio: %v", err)
	}
	if err := os.WriteFile(localInput, inputData, 0644); err != nil {
		t.Fatalf("copy sample audio: %v", err)
	}

	outputDir := filepath.Join(workDir, "runtime")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}

	var mu sync.Mutex
	var uploadedKeys []string
	var patchedJobID string
	var patchedOutput string

	// Mock S3 and API servers
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		// MinIO S3 PutObject or HeadObject
		if r.Header.Get("X-Amz-Date") != "" || strings.Contains(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256") || strings.Contains(r.URL.Path, "test-bucket") {
			if r.Method == http.MethodPut {
				uploadedKeys = append(uploadedKeys, r.URL.Path)
				w.WriteHeader(http.StatusOK)
				return
			}
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusOK)
				return
			}
		}

		// API PATCH /audio/{id}
		if r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/audio/") {
			patchedJobID = strings.TrimPrefix(r.URL.Path, "/audio/")
			body, _ := io.ReadAll(r.Body)
			var payload struct {
				OutputPath string `json:"output_path"`
			}
			_ = json.Unmarshal(body, &payload)
			patchedOutput = payload.OutputPath
			w.WriteHeader(http.StatusNoContent)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer mockServer.Close()

	t.Setenv("FILE_DOWNLOAD_LOCATION", outputDir)
	t.Setenv("S3_ENDPOINT_URL", mockServer.URL)
	t.Setenv("WEB_API_URL", mockServer.URL)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Construct SQS message
	msgBody := fmt.Sprintf(`{"job_id": "%s", "input_path": "%s"}`, jobID, localInput)
	msg := types.Message{
		Body: aws.String(msgBody),
		MessageAttributes: map[string]types.MessageAttributeValue{
			"content_type": {
				DataType:    aws.String("String"),
				StringValue: aws.String("application/json"),
			},
		},
	}

	ctx := context.Background()
	err = wrapperForCreateHlsStream(msg, ctx, logger)
	if err != nil {
		t.Fatalf("wrapperForCreateHlsStream error: %v", err)
	}

	// Verify HLS playlist generated locally
	hlsPlaylist := filepath.Join(outputDir, "hls", jobID, "playlist.m3u8")
	playlistBytes, err := os.ReadFile(hlsPlaylist)
	if err != nil {
		t.Fatalf("expected playlist file at %s: %v", hlsPlaylist, err)
	}
	playlistContent := string(playlistBytes)
	if !strings.Contains(playlistContent, "#EXTM3U") {
		t.Errorf("playlist missing #EXTM3U header: %s", playlistContent)
	}
	if !strings.Contains(playlistContent, ".ts") {
		t.Errorf("playlist missing .ts segment references: %s", playlistContent)
	}

	// Verify API was notified
	mu.Lock()
	defer mu.Unlock()
	if patchedJobID != jobID {
		t.Errorf("expected API PATCH for job %s, got %s", jobID, patchedJobID)
	}
	if !strings.Contains(patchedOutput, "playlist.m3u8") {
		t.Errorf("expected patched output to contain playlist.m3u8, got %s", patchedOutput)
	}

	// Verify S3 uploads took place
	var foundPlaylistUpload bool
	for _, key := range uploadedKeys {
		if strings.Contains(key, "playlist.m3u8") {
			foundPlaylistUpload = true
			break
		}
	}
	if !foundPlaylistUpload {
		t.Errorf("expected S3 upload for playlist.m3u8, uploaded keys: %v", uploadedKeys)
	}
}

func TestWrapperForCreateHlsStreamRejectsNonJson(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	msg := types.Message{
		Body: aws.String("raw message"),
		MessageAttributes: map[string]types.MessageAttributeValue{
			"content_type": {
				DataType:    aws.String("String"),
				StringValue: aws.String("text/plain"),
			},
		},
	}

	err := wrapperForCreateHlsStream(msg, ctx, logger)
	if err == nil {
		t.Fatalf("expected error for non-json message, got nil")
	}
	if !strings.Contains(err.Error(), "not a json message") {
		t.Errorf("expected error message to contain 'not a json message', got: %v", err)
	}
}

func TestWrapperForCreateHlsStreamRejectsMissingContentType(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	msg := types.Message{
		Body:              aws.String("raw message"),
		MessageAttributes: map[string]types.MessageAttributeValue{},
	}

	err := wrapperForCreateHlsStream(msg, ctx, logger)
	if err == nil {
		t.Fatalf("expected error for missing content_type, got nil")
	}
}
