package utils

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type WorkerJob struct{}

type FileFetcher interface {
	Fetch(ctx context.Context, uri string) (localPath string, cleanup func(), err error)
}

type LocalFileFetcher struct{
	logger *slog.Logger
}

func (l LocalFileFetcher) Fetch(ctx context.Context, uri string) (string, func(), error) {
	return "", func() {}, nil
}

type S3FileFetcher struct{
	logger *slog.Logger
}

func (l S3FileFetcher) Fetch(ctx context.Context, uri string) (string, func(), error) {
	cleanup := func() {}
	url, _ := url.Parse(uri)
	l.logger.Info("Source Path is remote server location. Downloading file...", "source_path", uri)
	resp, err := http.Get(url.String())
	if err != nil {
		l.logger.Error("Request failed", "err", err)
		return "", cleanup, err
	}
	defer resp.Body.Close()
	cleanPath := strings.Trim(url.Path, "/")
	segments := strings.Split(cleanPath, "/")
	// TODO: use os.CreateTemp instead
	tempFileDirectory := "/tmp/" + segments[len(segments)-2]
	tempFileLocation := tempFileDirectory + "/" + segments[len(segments)-1]
	dir := filepath.Dir(tempFileLocation)
	err = os.MkdirAll(dir, 0755)
	cleanup = func() {
		os.RemoveAll(tempFileLocation)
	}
	if err != nil {
		cleanup()
		l.logger.Error("Failed to create directory", "directory", dir, "err", err)
		return "", cleanup, err
	}
	file, err := os.Create(tempFileLocation)
	if err != nil {
		cleanup()
		l.logger.Error("Unable to create local file", "err", err)
		return "", cleanup, err
	}
	_, err = io.Copy(file, resp.Body)
	if err != nil {
		cleanup()
		l.logger.Error("Failed to save file contents", "err", err)
		return "", cleanup, err
	}
	l.logger.Info("Successfully downloaded", "local_file_location", tempFileLocation)
	return tempFileLocation, cleanup, nil
}

type UniversalFetcher struct {
	local FileFetcher
	s3    FileFetcher
}

func CreateHlsStream(srcPath string, ctx context.Context, logger *slog.Logger) error {
	universalFetcher := UniversalFetcher{
		local: LocalFileFetcher{
			logger: logger,
		},
		s3: S3FileFetcher{
			logger: logger,
		},
	}
	if !filepath.IsAbs(srcPath) {
		localPath, cleanup, err := universalFetcher.s3.Fetch(ctx, srcPath)
		if err != nil {
			return fmt.Errorf("fetching s3 object %q: %w", srcPath, err)
		}
		defer cleanup()
		srcPath = localPath
	}
	fileDir := filepath.Dir(srcPath)
	fileFolder := filepath.Base(fileDir)
	dir := os.Getenv("FILE_DOWNLOAD_LOCATION")
	_, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("directory does not exists %q: %w", dir, err)
		}
		return fmt.Errorf("checking directory %q: %w", dir, err)
	}
	targetDir := filepath.Join(dir, "hls", fileFolder)
	hlsPath := filepath.Join(targetDir, "playlist.m3u8")
	logger.Info("paths", "targetDir", targetDir, "hlsPath", hlsPath)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("creating HLS directory %q: %w", targetDir, err)
	}
	chunk_size := 20
	args := []string{
		"-i",
		srcPath,
		"-map",
		"0:a",
		"-codec:",
		"copy",
		"-start_number",
		"0",
		"-hls_time",
		strconv.Itoa(chunk_size),
		"-hls_list_size",
		"0",
		"-f",
		"hls",
		hlsPath,
  }
  cmd := exec.CommandContext(ctx, "ffmpeg", args...);
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	started := time.Now()
	logger.Info("ffmpeg started", "input", srcPath, "output", hlsPath)
	if err := cmd.Run(); err != nil {
		logger.Error("ffmpeg failed", "err", err, "commandErr", strings.TrimSpace(stderr.String()))
		return err
	}
	logger.Info("ffmpeg completed", "output", hlsPath, "duration", time.Since(started).Round(time.Millisecond))

	audioJobUpdate := &AudioJobUpdate{
		OutputPath: hlsPath,
	}
	// TODO: add validation for uuid.
	s3Client := CreateS3Client(ctx, logger)
	files, err := os.ReadDir(targetDir)
	if err != nil {
		return fmt.Errorf("reading files directory %q: %w", targetDir, err)
	}
	for _, file := range files {
		// Added as a failsafe.
		if file.Type().IsDir() {
			continue
		}
		err = s3Client.UploadFile(ctx, "test-bucket", filepath.Join(fileFolder, file.Name()), filepath.Join(targetDir, file.Name()))
		if err != nil {
			return fmt.Errorf("uploading file %q: %w", file.Name(), err)
		}
	}
	// TODO: change to s3 path.
	UpdateAudioJob(fileFolder, audioJobUpdate, logger)
	return nil
}

func CreateInfinitelyConsumingWorkerPool[T any](numWorkers int, workerJob func(work T, ctx context.Context, logger *slog.Logger)error, works <-chan T, finishedWorks chan<- T, logger *slog.Logger, ctx context.Context) *sync.WaitGroup {
	var wg sync.WaitGroup
	if numWorkers <= 0 {
		return &wg
	}
	wg.Add(numWorkers)
	for range numWorkers {
		go func() {
			for work := range works {
				// TODO: create a timer to cancel automatically to avoid stale workers.
				logger.Debug("Starting to process work", "work", work)
				err := workerJob(work, ctx, logger)
				if err != nil {
					logger.Error("Failed processing work", "work", work, "error", err)
					continue
				}
				logger.Debug("Finished processing work", "work", work)
				finishedWorks <- work
			}
			wg.Done()
		}()
	}
	return &wg
}
