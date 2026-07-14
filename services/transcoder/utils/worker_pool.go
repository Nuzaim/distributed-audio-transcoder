package utils

import (
	"bytes"
	"context"
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

func CreateHlsStream(srcPath string, ctx context.Context, logger *slog.Logger) error {
	// TODO: refactor.
	if !filepath.IsAbs(srcPath) {
		url, _ := url.Parse(srcPath)
		logger.Info("Source Path is remote server location. Downloading file...", "source_path", srcPath)
		resp, err := http.Get(url.String())
		if err != nil {
			logger.Error("Request failed", "err", err)
			return err
		}
		defer resp.Body.Close()
		cleanPath := strings.Trim(url.Path, "/")
		segments := strings.Split(cleanPath, "/")
		tempFileDirectory := "/tmp/" + segments[len(segments)-2]
		tempFileLocation := tempFileDirectory + "/" + segments[len(segments)-1]
		dir := filepath.Dir(tempFileLocation)
		err = os.MkdirAll(dir, 0755)
		if err != nil {
			logger.Error("Failed to create directory", "directory", dir, "err", err)
			return err
		}
		file, err := os.Create(tempFileLocation)
		if err != nil {
			logger.Error("Unable to create local file", "err", err)
			return err
		}
		_, err = io.Copy(file, resp.Body)
		if err != nil {
			logger.Error("Failed to save file contents", "err", err)
			return err
		}
		logger.Info("Successfully downloaded", "local_file_location", tempFileLocation)
		srcPath = tempFileLocation
	}
	fileDir := filepath.Dir(srcPath)
	fileFolder := filepath.Base(fileDir)
	dir := os.Getenv("FILE_DOWNLOAD_LOCATION")
	_, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Error("Directory does not exist!")
			return err
		}
		logger.Error("Error checking directory", "directory", dir, "err", err)
		return err
	}
	targetDir := filepath.Join(dir, "hls", fileFolder)
	hlsPath := filepath.Join(targetDir, "playlist.m3u8")
	logger.Info("paths", "targetDir", targetDir, "hlsPath", hlsPath)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		logger.Error("Failed to create HLS directory", "err", err)
    return err
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
		logger.Error("Failed to get files in the directory", "directory", targetDir)
		return err
	}
	for _, file := range files {
		// Added as a failsafe.
		if file.Type().IsDir() {
			continue
		}
		s3Client.UploadFile(ctx, "test-bucket", filepath.Join(fileFolder, file.Name()), filepath.Join(targetDir, file.Name()))
	}
	// TODO: change to s3 path.
	UpdateAudioJob(fileFolder, audioJobUpdate, logger)
	return nil
}

func CreateInfinitelyConsumingWorkerPool[T any](numWorkers int, workerJob func(work T, ctx context.Context, logger *slog.Logger)error, works <-chan T, logger *slog.Logger, ctx context.Context) *sync.WaitGroup {
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
				workerJob(work, ctx, logger)
				logger.Debug("Finished processing work", "work", work)
			}
			wg.Done()
		}()
	}
	return &wg
}
