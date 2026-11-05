package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/signal"
	"sync"
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
	messagesChan := make(chan types.Message, 5)
	processedMessages := make(chan types.Message, 5)
	numPoolWorkers := 4
	wg := utils.CreateInfinitelyConsumingWorkerPool(numPoolWorkers, wrapperForCreateHlsStream, messagesChan, processedMessages, logger, ctx)
	logger.Info("Worker pool created", "PoolWorkers", numPoolWorkers)
	// WARNING: Change.
	sqsClient := utils.CreateSqsClient(ctx, logger)
	go func(){
		<-ctx.Done()
		close(messagesChan)
		logger.Info("Channel closed")
	}()
	var deletionWg sync.WaitGroup
	deletionWg.Go(func(){
		for processedMessage := range processedMessages {
			delCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			err := sqsClient.DeleteMessage(delCtx, "/02-queue-with-dead-letter", processedMessage)
			cancel()
			if err != nil {
				logger.Error("Failed to delete finished message from queue", "err", err, "messageId", processedMessage.MessageId)
				continue
			}
			logger.Info("Deleted the finished message", "messageId", processedMessage.MessageId)
		}
	})
	for {
		messages, err := sqsClient.GetMessages(ctx, "/02-queue-with-dead-letter", 2, 2)
		if err!=nil{
			if ctx.Err()!=nil {
				logger.Info("context cancelled", "err", err)
				// TODO: reset visibility
				// make an interface for this.
				break
			}
			logger.Error("get messages from SQS queue", "err", err)
			continue
		}
		for _, message := range messages{
			messagesChan <- message
		}
	}
	wg.Wait()
	deletionWg.Wait()
}
