package utils

import (
	"os"
	"sync"
	"time"
	"bytes"
	"context"
	"strconv"
	"strings"
	"os/exec"
	"log/slog"
	"path/filepath"
)

type WorkerJob struct{}

func CreateHlsStream(srcPath string, ctx context.Context, logger *slog.Logger) error {
	ext := filepath.Ext(srcPath)
	filename := filepath.Base(srcPath)
	baseName := strings.TrimSuffix(filename, ext)
	dir, err := os.Getwd()
	if err != nil {
		logger.Error("Failed to get working directory", "err", err)
    return err
	}
	targetDir := filepath.Join(dir, "hls", baseName)
	hlsPath := filepath.Join(targetDir, "playlist.m3u8")
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
	filenameSplit := strings.Split(baseName, "_")
	UpdateAudioJob(filenameSplit[0], audioJobUpdate, logger)
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
