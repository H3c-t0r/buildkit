package agent

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
)

type S3Downloader struct {
	// The S3 bucket name and the path, e.g s3://my-bucket-name/foo/bar
	Bucket string

	// The root directory of the download
	Destination string

	// The relative path that should be preserved in the download folder,
	// also it's location in the bucket
	Path string

	// How many times should it retry the download before giving up
	Retries int

	// If failed responses should be dumped to the log
	DebugHTTP bool
}

func (d S3Downloader) Start() error {
	// Initialize the s3 client, and authenticate it
	s3Client, err := newS3Client(d.BucketName())
	if err != nil {
		return err
	}

	req, _ := s3Client.GetObjectRequest(&s3.GetObjectInput{
		Bucket: aws.String(d.BucketName()),
		Key:    aws.String(d.BucketFileLocation()),
	})

	signedURL, err := req.Presign(time.Hour)
	if err != nil {
		return fmt.Errorf("error pre-signing request: %v", err)
	}

	// We can now cheat and pass the URL onto our regular downloader
	return Download{
		Client:      *http.DefaultClient,
		URL:         signedURL,
		Path:        d.Path,
		Destination: d.Destination,
		Retries:     d.Retries,
		DebugHTTP:   d.DebugHTTP,
	}.Start()
}

func (d S3Downloader) BucketFileLocation() string {
	if d.BucketPath() != "" {
		return strings.TrimSuffix(d.BucketPath(), "/") + "/" + strings.TrimPrefix(d.Path, "/")
	} else {
		return d.Path
	}
}

func (d S3Downloader) BucketPath() string {
	return strings.Join(d.destinationParts()[1:len(d.destinationParts())], "/")
}

func (d S3Downloader) BucketName() string {
	return d.destinationParts()[0]
}

func (d S3Downloader) destinationParts() []string {
	trimmed := strings.TrimPrefix(d.Bucket, "s3://")

	return strings.Split(trimmed, "/")
}
