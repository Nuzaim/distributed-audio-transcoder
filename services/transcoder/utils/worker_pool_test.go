package utils

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type failingReadCloser struct {
	data []byte
	err  error
	sent bool
}

func (r *failingReadCloser) Read(p []byte) (int, error) {
	if r.sent {
		return 0, r.err
	}
	r.sent = true
	n := copy(p, r.data)
	return n, nil
}

func (*failingReadCloser) Close() error { return nil }

func TestS3FileFetcherFetchDownloadsObjectAndCleanup(t *testing.T) {
	objectBody := []byte("audio file contents")
	objectDirectory := testObjectDirectory(t)
	expectedPath := "/bucket/" + objectDirectory + "/source.mp3"

	setDefaultTransport(t, roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Errorf("request method = %q, want %q", req.Method, http.MethodGet)
		}
		if req.URL.Path != expectedPath {
			t.Errorf("request path = %q, want %q", req.URL.Path, expectedPath)
		}
		if got := req.URL.Query().Get("X-Amz-Signature"); got != "test-signature" {
			t.Errorf("signature query = %q, want %q", got, "test-signature")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(objectBody)),
			Request:    req,
		}, nil
	}))

	fetcher := testS3FileFetcher()
	uri := "https://storage.example.test" + expectedPath + "?X-Amz-Signature=test-signature"

	localPath, cleanup, err := fetcher.Fetch(context.Background(), uri)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if localPath == "" {
		t.Fatal("Fetch() returned an empty local path")
	}
	defer cleanup()
	defer os.RemoveAll(filepath.Join(os.TempDir(), objectDirectory))

	got, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("reading fetched file: %v", err)
	}
	if !bytes.Equal(got, objectBody) {
		t.Errorf("fetched contents = %q, want %q", got, objectBody)
	}

	cleanup()
	if _, err := os.Stat(localPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file still exists after cleanup; stat error = %v", err)
	}
}

func TestS3FileFetcherFetchReturnsRequestError(t *testing.T) {
	objectDirectory := testObjectDirectory(t)
	defer os.RemoveAll(filepath.Join(os.TempDir(), objectDirectory))

	setDefaultTransport(t, roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection failed")
	}))

	fetcher := testS3FileFetcher()
	uri := "https://storage.example.test/bucket/" + objectDirectory + "/missing.mp3"
	localPath, cleanup, err := fetcher.Fetch(context.Background(), uri)
	if err == nil {
		t.Fatal("Fetch() error = nil, want a request error")
	}
	if localPath != "" {
		t.Errorf("Fetch() path = %q, want empty path on error", localPath)
	}
	cleanup()
}

func TestS3FileFetcherFetchCleansPartialFileAfterCopyError(t *testing.T) {
	objectDirectory := testObjectDirectory(t)
	defer os.RemoveAll(filepath.Join(os.TempDir(), objectDirectory))

	setDefaultTransport(t, roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       &failingReadCloser{data: []byte("partial body"), err: errors.New("read failed")},
			Request:    req,
		}, nil
	}))

	fetcher := testS3FileFetcher()
	uri := "https://storage.example.test/bucket/" + objectDirectory + "/partial.mp3"
	localPath, cleanup, err := fetcher.Fetch(context.Background(), uri)
	if err == nil {
		t.Fatal("Fetch() error = nil, want a copy error")
	}
	if localPath != "" {
		t.Errorf("Fetch() path = %q, want empty path on error", localPath)
	}
	cleanup()

	partialPath := filepath.Join(os.TempDir(), objectDirectory, "partial.mp3")
	if _, err := os.Stat(partialPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("partial file still exists after cleanup; stat error = %v", err)
	}
}

func setDefaultTransport(t *testing.T, transport http.RoundTripper) {
	t.Helper()

	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() {
		http.DefaultTransport = previous
	})
}

func testS3FileFetcher() S3FileFetcher {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return S3FileFetcher{logger: logger}
}

func testObjectDirectory(t *testing.T) string {
	t.Helper()

	testTempDir := t.TempDir()
	return filepath.Base(filepath.Dir(testTempDir)) + "-" + filepath.Base(testTempDir)
}
