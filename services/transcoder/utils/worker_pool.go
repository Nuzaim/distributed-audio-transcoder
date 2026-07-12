package utils

import (
	"log/slog"
	"sync"
	"time"
	"context"
)

type WorkerJob struct{}

func Transcode(filepath string, ctx context.Context, logger *slog.Logger) error {
	logger.Info("Starting to process filepath", "path", filepath)
	time.Sleep(2 * time.Millisecond)
	logger.Info("Finished processing filepath", "path", filepath)
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
				logger.Debug("Starting to process work", "work", work)
				workerJob(work, ctx, logger)
				logger.Debug("Finished processing work", "work", work)
			}
			wg.Done()
		}()
	}
	return &wg
}
