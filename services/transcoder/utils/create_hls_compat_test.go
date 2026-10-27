package utils

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type pipelineRoundTripper func(*http.Request) (*http.Response, error)

func (rt pipelineRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return rt(req)
}

func TestCreateHlsStreamRemoteInputPreservesPipeline(t *testing.T) {
	workDir := t.TempDir()
	objectDirectory := filepath.Base(filepath.Dir(workDir)) + "-" + filepath.Base(workDir)
	defer os.RemoveAll(filepath.Join(os.TempDir(), objectDirectory))

	outputRoot := filepath.Join(workDir, "output")
	if err := os.MkdirAll(outputRoot, 0755); err != nil {
		t.Fatalf("create output root: %v", err)
	}

	binDir := filepath.Join(workDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create fake ffmpeg directory: %v", err)
	}
	inputLog := filepath.Join(workDir, "ffmpeg-input")
	ffmpeg := "#!/bin/sh\n" +
		"input=\noutput=\n" +
		"while [ \"$#\" -gt 0 ]; do\n" +
		"  if [ \"$1\" = \"-i\" ]; then\n" +
		"    shift\n" +
		"    input=$1\n" +
		"    [ -f \"$input\" ] || exit 17\n" +
		"    [ \"$(cat \"$input\")\" = \"source-audio\" ] || exit 18\n" +
		"  fi\n" +
		"  output=$1\n" +
		"  shift\n" +
		"done\n" +
		"printf '%s\\n%s' \"$input\" \"$output\" > \"$FFMPEG_INPUT_LOG\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "ffmpeg"), []byte(ffmpeg), 0755); err != nil {
		t.Fatalf("write fake ffmpeg: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FFMPEG_INPUT_LOG", inputLog)
	t.Setenv("FILE_DOWNLOAD_LOCATION", outputRoot)
	t.Setenv("S3_ENDPOINT_URL", "http://storage.test")
	t.Setenv("WEB_API_URL", "http://api.test")

	sourcePath := "/bucket/" + objectDirectory + "/input.mp3"
	sourceURI := "https://source.test" + sourcePath + "?signature=test"
	var sourceRequested bool
	var completionPath string
	var completionOutput string

	previousTransport := http.DefaultTransport
	http.DefaultTransport = pipelineRoundTripper(func(req *http.Request) (*http.Response, error) {
		response := func(status int, body string) *http.Response {
			return &http.Response{
				StatusCode: status,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}
		}

		switch req.URL.Host {
		case "source.test":
			sourceRequested = true
			if req.Method != http.MethodGet || req.URL.Path != sourcePath || req.URL.Query().Get("signature") != "test" {
				t.Errorf("unexpected source request: %s %s", req.Method, req.URL.String())
			}
			return response(http.StatusOK, "source-audio"), nil
		case "storage.test":
			return nil, &pipelineUnexpectedRequestError{method: req.Method, url: req.URL.String()}
		case "api.test":
			completionPath = req.URL.Path
			body, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			var update AudioJobUpdate
			if err := json.Unmarshal(body, &update); err != nil {
				return nil, err
			}
			completionOutput = update.OutputPath
			return response(http.StatusOK, "{}"), nil
		default:
			return nil, &pipelineUnexpectedRequestError{method: req.Method, url: req.URL.String()}
		}
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := CreateHlsStream(sourceURI, context.Background(), logger); err != nil {
		t.Fatalf("CreateHlsStream() error = %v", err)
	}

	if !sourceRequested {
		t.Error("remote source was not requested")
	}
	wantInputPath := filepath.Join(os.TempDir(), objectDirectory, "input.mp3")
	wantOutputPath := filepath.Join(outputRoot, "hls", objectDirectory, "playlist.m3u8")
	gotInputPath, err := os.ReadFile(inputLog)
	if err != nil {
		t.Fatalf("read ffmpeg input log: %v", err)
	}
	ffmpegPaths := strings.SplitN(string(gotInputPath), "\n", 2)
	if len(ffmpegPaths) != 2 {
		t.Fatalf("ffmpeg path log = %q, want input and output paths", gotInputPath)
	}
	if ffmpegPaths[0] != wantInputPath {
		t.Errorf("ffmpeg input path = %q, want %q", ffmpegPaths[0], wantInputPath)
	}
	if ffmpegPaths[1] != wantOutputPath {
		t.Errorf("ffmpeg output path = %q, want %q", ffmpegPaths[1], wantOutputPath)
	}

	if completionPath != "/audio/"+objectDirectory {
		t.Errorf("completion API path = %q, want %q", completionPath, "/audio/"+objectDirectory)
	}
	if completionOutput != wantOutputPath {
		t.Errorf("completion output path = %q, want %q", completionOutput, wantOutputPath)
	}

}

type pipelineUnexpectedRequestError struct {
	method string
	url    string
}

func (err *pipelineUnexpectedRequestError) Error() string {
	return "unexpected HTTP request: " + err.method + " " + err.url
}
