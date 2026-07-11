package utils

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

type SqsActions struct {
	SqsClient *sqs.Client
	logger *slog.Logger
}

func (actor SqsActions) GetMessages(ctx context.Context, queueUrl string, maxMessages int32, waitTime int32) ([]types.Message, error) {
	var messages []types.Message
	result, err := actor.SqsClient.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(queueUrl),
		MaxNumberOfMessages: maxMessages,
		WaitTimeSeconds:     waitTime,
		MessageAttributeNames: []string{"All"},
		AttributeNames: []types.QueueAttributeName{"All"},
	})
	if err != nil {
		actor.logger.Error("Couldn't get messages from queue", "QueueUrl", queueUrl, "err", err)
	} else {
		messages = result.Messages
	}
	return messages, err
}

func (actor SqsActions) DeleteMessages(ctx context.Context, queueUrl string, messages []types.Message) error {
	entries := make([]types.DeleteMessageBatchRequestEntry, len(messages))
	for msgIndex := range messages {
		entries[msgIndex].Id = aws.String(fmt.Sprintf("%v", msgIndex))
		entries[msgIndex].ReceiptHandle = messages[msgIndex].ReceiptHandle
	}
	_, err := actor.SqsClient.DeleteMessageBatch(ctx, &sqs.DeleteMessageBatchInput{
		Entries:  entries,
		QueueUrl: aws.String(queueUrl),
	})
	if err != nil {
		actor.logger.Error("Couldn't delete messages from queue", "QueueUrl", queueUrl, "err", err)
	}
	return err
}

func CreateSqsClient(ctx context.Context, logger *slog.Logger) *SqsActions{
  sdkConfig, err := config.LoadDefaultConfig(
  	ctx,
  	config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("AKID", "SECRET_KEY", "TOKEN"),),
  	config.WithRegion("us-east-1"),
  )
	if err != nil {
		logger.Error("Couldn't load default configuration. Have you set up your AWS account?", "err", err)
	}
	sqsClient := sqs.NewFromConfig(
		sdkConfig, 
		func (o *sqs.Options) {
    		o.BaseEndpoint = aws.String("http://localhost:9324")
		},
	)
  sqsClientImpl := SqsActions{sqsClient, logger}
  return &sqsClientImpl
}
