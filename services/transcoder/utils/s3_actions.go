package utils

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

type S3Actions struct {
	S3Client *s3.Client
	logger   *slog.Logger
}

func (actor S3Actions) DownloadFile(ctx context.Context, bucketName string, objectKey string, fileName string) error {
	result, err := actor.S3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		var noKey *types.NoSuchKey
		if errors.As(err, &noKey) {
			actor.logger.Error("Can't get object from bucket. No such key exists.", "object_key", objectKey, "bucket_name", bucketName)
			err = noKey
		} else {
			actor.logger.Error("Couldn't get object.", "object", bucketName+objectKey, "err", err)
		}
		return err
	}
	defer result.Body.Close()
	file, err := os.Create(fileName)
	if err != nil {
		actor.logger.Error("Couldn't create file.", "file_path", fileName, "err", err)
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, result.Body)
	if err != nil {
		actor.logger.Error("Couldn't read object body", "object_key", objectKey, "err", err)
		return err
	}
	actor.logger.Info("Successfully downloaded", "local_file_location", fileName)
	return err
}

func (actor S3Actions) UploadFile(ctx context.Context, bucketName string, objectKey string, fileName string) error {
	actor.logger.Info("Uploading file to S3", "file_name", fileName, "bucket_name", bucketName, "object_key", objectKey)
	file, err := os.Open(fileName)
	if err != nil {
		actor.logger.Error("Couldn't open file to upload", "file_name", fileName, "err", err)
		return err
	}
	defer file.Close()
	contentType := getContentType(fileName)
	actor.logger.Info("Content type detected", "content_type", contentType)
	_, err = actor.S3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucketName),
		Key:         aws.String(objectKey),
		Body:        file,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "EntityTooLarge" {
			actor.logger.Error("Error while uploading object to bucket. The object is too large.\n"+
				"To upload objects larger than 5GB, use the S3 console (160GB max)\n"+
				"or the multipart upload API (5TB max).", "bucket_name", bucketName)
		} else {
			actor.logger.Error("Couldn't upload file", "file_name", fileName, "bucket_name", bucketName, "object_key", objectKey, "err", err)
		}
	} else {
		err = s3.NewObjectExistsWaiter(actor.S3Client).Wait(
			ctx, &s3.HeadObjectInput{Bucket: aws.String(bucketName), Key: aws.String(objectKey)}, time.Minute)
		if err != nil {
			actor.logger.Error("Failed attempt to wait for object to exist.\n", "object_key", objectKey)
		}
	}
	actor.logger.Info("Upload Task initiated successfully")
	// NOTE: need to wait to see if the file is uploaded?
	return err
}

func getContentType(fileName string) string {
	contentType := mime.TypeByExtension(filepath.Ext(fileName))
	if contentType == "" {
		// Fallback defaults
		if filepath.Ext(fileName) == ".m3u8" {
			contentType = "application/x-mpegURL"
		} else if filepath.Ext(fileName) == ".ts" {
			contentType = "video/MP2T"
		} else {
			contentType = "application/octet-stream"
		}
	}
	return contentType
}

func CreateS3Client(ctx context.Context, logger *slog.Logger) *S3Actions {
	// TODO: check if a client factory can be made.
	sdkConfig, err := config.LoadDefaultConfig(
		ctx,
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("minioadmin", "minioadminpassword", "")),
		config.WithRegion("us-east-1"),
	)
	if err != nil {
		logger.Error("Couldn't load default configuration. Have you set up your AWS account?", "err", err)
	}
	s3Client := s3.NewFromConfig(
		sdkConfig,
		func(o *s3.Options) {
			endpoint_url := os.Getenv("S3_ENDPOINT_URL")
			if endpoint_url == "" {
				endpoint_url = "http://localhost:9000"
			}
			o.BaseEndpoint = aws.String(endpoint_url)
			o.UsePathStyle = true // NOTE: might need to change when using s3 service.
		},
	)
	s3ClientImpl := S3Actions{s3Client, logger}
	return &s3ClientImpl
}
