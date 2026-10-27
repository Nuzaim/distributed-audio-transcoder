package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os/signal"
	"syscall"
	"transcoder-service/utils"

	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

type sqsMessageBody struct	{
	Job_id string `json:"job_id"`
	Input_path string `json:"input_path"`
}

func wrapperForCreateHlsStream(message types.Message, ctx context.Context, logger *slog.Logger) error {
	attr, ok := message.MessageAttributes["content_type"]
	if !ok || attr.StringValue == nil {
		return fmt.Errorf("missing or empty content_type message attribute")
	}
	if *attr.StringValue!="application/json"{
		return fmt.Errorf("not a json message")
	}
	if message.Body == nil {
		return fmt.Errorf("missing SQS message body")
	}
	messageBody := &sqsMessageBody{}
	err := json.Unmarshal([]byte(*message.Body), messageBody)
	if err != nil{
		return fmt.Errorf("decoding message body failed: %w", err)
	}
	if messageBody.Input_path == "" {
		return fmt.Errorf("Empty path in message body: %w", err)
	}
	return utils.CreateHlsStream(messageBody.Input_path, ctx, logger)
}

func main() {
	logger := utils.NewLogger()
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	// TODO:
	filepathsChan := make(chan types.Message, 5)
	processedMessages := make(chan types.Message, 5)
	numPoolWorkers := 4
	wg := utils.CreateInfinitelyConsumingWorkerPool(numPoolWorkers, wrapperForCreateHlsStream, filepathsChan, processedMessages, logger, ctx)
	logger.Info("Worker pool created", "PoolWorkers", numPoolWorkers)
	// WARNING: Change.
	sqsClient := utils.CreateSqsClient(ctx, logger)
	go func(){
		<-ctx.Done()
		close(filepathsChan)
		// close(processedMessages)
		logger.Info("Channel closed")
		wg.Wait()
	}()
	go func(){
		for processedMessage := range processedMessages {
			sqsClient.DeleteMessages(ctx, "/02-queue-with-dead-letter", []types.Message{processedMessage})
			logger.Info("Deleted the finished message", "processedMessage", processedMessage)
		}
	}()
	for {
		messages, err := sqsClient.GetMessages(ctx, "/02-queue-with-dead-letter", 2, 2)
		if err!=nil{
			// ???
			// exits immediately?
			log.Fatal(err)
		}
		for _, message := range messages{
			filepathsChan <- message
		}
		// sqsClient.DeleteMessages(ctx, "/02-queue-with-dead-letter", messages)
	}
}
