package main

import (
	"context"
	"encoding/json"
	"log"
	"log/slog"
	"os/signal"
	"syscall"
	"transcoder-service/utils"
)

type sqsMessageBody struct	{
	Job_id string `json:"job_id"`
	Input_path string `json:"input_path"`
}

func main() {
	logger := utils.NewLogger()
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	filepathsChan := make(chan string, 5)
	wg := utils.CreateInfinitelyConsumingWorkerPool(4, utils.Transcode, filepathsChan, logger)
	// WARNING: Change.
	sqsClient := utils.CreateSqsClient(ctx, logger)
	go func(){
		<-ctx.Done()
		close(filepathsChan)
		logger.Info("Channel closed")
		wg.Wait()
	}()
	for {
		messages, err := sqsClient.GetMessages(ctx, "/02-queue-with-dead-letter", 2, 2)
		if err!=nil{
			log.Fatal(err)
		}
		for _, message := range messages{
			if *message.MessageAttributes["content_type"].StringValue!="application/json"{
				continue
			}
			messageBody := &sqsMessageBody{}
			err := json.Unmarshal([]byte(*message.Body), messageBody)
			if err != nil{
				logger.Error("Failed json unmarshal", "err", err) // TODO: refactor.
			}
			filepathsChan <- messageBody.Input_path
		}
		sqsClient.DeleteMessages(ctx, "/02-queue-with-dead-letter", messages)
	}
}
