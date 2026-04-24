// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
//
// See LICENSE file for details.

package backup

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3 struct {
	storagePath string
	user        string
	bucket      string
	client      *s3.Client
	endpoint    string
	region      string
}

func NewS3(storagePath, user string, opts map[string]string) *S3 {
	bucket := user + "_smriti"
	if b, ok := opts["s3_bucket"]; ok && b != "" {
		bucket = b
	}
	return &S3{
		storagePath: storagePath,
		user:        user,
		bucket:      bucket,
		endpoint:    opts["s3_endpoint"],
		region:      opts["s3_region"],
	}
}

func (s *S3) Init(ctx context.Context) error {
	client, err := s.createClient(ctx)
	if err != nil {
		return fmt.Errorf("create s3 client: %w", err)
	}
	s.client = client

	_, err = s.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(s.bucket),
	})
	if err != nil {
		_, createErr := s.client.CreateBucket(ctx, &s3.CreateBucketInput{
			Bucket: aws.String(s.bucket),
		})
		if createErr != nil {
			return fmt.Errorf("create bucket %s: %w", s.bucket, createErr)
		}
	}
	return nil
}

func (s *S3) createClient(ctx context.Context) (*s3.Client, error) {
	region := s.region
	if region == "" {
		region = "us-east-1"
	}

	var opts []func(*config.LoadOptions) error
	opts = append(opts, config.WithRegion(region))

	if accessKey := os.Getenv("AWS_ACCESS_KEY_ID"); accessKey != "" {
		secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, err
	}

	var s3Opts []func(*s3.Options)
	if s.endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(s.endpoint)
			o.UsePathStyle = true
		})
	}

	return s3.NewFromConfig(cfg, s3Opts...), nil
}

func (s *S3) Pull(ctx context.Context) error {
	if s.client == nil {
		return fmt.Errorf("s3 client not initialized")
	}

	listOutput, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
	})
	if err != nil {
		return fmt.Errorf("list objects: %w", err)
	}

	for _, obj := range listOutput.Contents {
		key := aws.ToString(obj.Key)
		localPath := filepath.Join(s.storagePath, key)

		if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
			return fmt.Errorf("create dir for %s: %w", key, err)
		}

		getOutput, err := s.client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(s.bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			return fmt.Errorf("get object %s: %w", key, err)
		}

		f, err := os.Create(localPath)
		if err != nil {
			getOutput.Body.Close()
			return fmt.Errorf("create file %s: %w", localPath, err)
		}
		_, err = io.Copy(f, getOutput.Body)
		getOutput.Body.Close()
		f.Close()
		if err != nil {
			return fmt.Errorf("write file %s: %w", localPath, err)
		}
	}
	return nil
}

func (s *S3) Push(ctx context.Context) error {
	if s.client == nil {
		return fmt.Errorf("s3 client not initialized")
	}

	remoteChecksums := make(map[string]string)
	listOutput, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
	})
	if err == nil {
		for _, obj := range listOutput.Contents {
			key := aws.ToString(obj.Key)
			etag := aws.ToString(obj.ETag)
			etag = strings.Trim(etag, "\"")
			remoteChecksums[key] = etag
		}
	}

	return filepath.Walk(s.storagePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(s.storagePath, path)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(relPath)

		localChecksum, err := fileMD5(path)
		if err != nil {
			return fmt.Errorf("checksum %s: %w", path, err)
		}

		if remoteEtag, ok := remoteChecksums[key]; ok && remoteEtag == localChecksum {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open %s: %w", path, err)
		}
		defer f.Close()

		_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(s.bucket),
			Key:    aws.String(key),
			Body:   f,
		})
		if err != nil {
			return fmt.Errorf("put object %s: %w", key, err)
		}
		return nil
	})
}

func (s *S3) Close() error {
	return nil
}

func fileMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
