package utils

import (
	"log/slog"
	"sync"
	"time"
)

type WorkerJob struct{}

func Transcode(filepath string, wg *sync.WaitGroup, logger *slog.Logger) {
	logger.Info("Starting to process filepath", "path", filepath)
	time.Sleep(2 * time.Millisecond)
	logger.Info("Finished processing filepath", "path", filepath)
}

func CreateInfinitelyConsumingWorkerPool[T any](numWorkers int, workerJob func(work T, waitGroup *sync.WaitGroup, logger *slog.Logger), works <-chan T, logger *slog.Logger) *sync.WaitGroup {
	var wg sync.WaitGroup
	if numWorkers <= 0 {
		return &wg
	}
	wg.Add(numWorkers)
	for range numWorkers {
		go func() {
			for work := range works {
				logger.Debug("Starting to process work", "work", work)
				workerJob(work, &wg, logger)
				logger.Debug("Finished processing work", "work", work)
			}
			wg.Done()
		}()
	}
	return &wg
}
