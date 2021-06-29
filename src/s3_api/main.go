package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/spf13/viper"
)

type Config struct {
	S3Bucket         string `mapstructure:"S3_BUCKET"`
	S3Endpoint       string `mapstructure:"S3_ENDPOINT"`
	S3DisableSsl     bool   `mapstructure:"S3_DISABLE_SSL"`
	S3ForcePathstyle bool   `mapstructure:"S3_FORCE_PATHSTYLE"`
	HttpPort         int    `mapstructure:"HTTP_PORT"`
}

func LoadConfig(path string) (config Config, err error) {
	viper.AddConfigPath(path)
	viper.SetConfigName("app")
	viper.SetConfigType("env")

	viper.AutomaticEnv()

	err = viper.ReadInConfig()
	if err != nil {
		return
	}

	err = viper.Unmarshal(&config)
	return
}

type Logger struct {
	handler http.Handler
}

func (l *Logger) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	l.handler.ServeHTTP(w, r)
	hn, _ := os.Hostname()
	log.Printf("%s: %s %s (%v)", hn, r.Method, r.URL.Path, time.Since(start))
}

func NewLogger(handlerToWrap http.Handler) *Logger {
	return &Logger{handlerToWrap}
}

// Driver docs https://docs.aws.amazon.com/sdk-for-go/v1/developer-guide/using-s3-with-go-sdk.html
func (config *Config) s3Handler(w http.ResponseWriter, r *http.Request) {
	s3Config := &aws.Config{
		Endpoint:         aws.String(config.S3Endpoint),
		DisableSSL:       aws.Bool(config.S3DisableSsl),
		S3ForcePathStyle: aws.Bool(config.S3ForcePathstyle),
	}

	sess := session.New(s3Config)
	s3Client := s3.New(sess)

	bucket := aws.String(config.S3Bucket)
	// FIXME this shouldn't be in prod, can assume that buckets are already created
	_, err := s3Client.CreateBucket(&s3.CreateBucketInput{
		Bucket: bucket,
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case s3.ErrCodeBucketAlreadyOwnedByYou:
				// do nothing, this is expected
			default:
				http.Error(w, fmt.Sprintf("Failed to upload data to %s, %s: %s\n", *bucket, aerr.Code(), aerr.Message()), 500)
				return
			}
		} else {
			fmt.Println(err.Error())
			return
		}
	}

	key := aws.String(fmt.Sprintf("%v.log", time.Now().UTC().Unix()))
	_, err = s3Client.PutObject(&s3.PutObjectInput{
		Body:   strings.NewReader("Hello from MinIO!!"),
		Bucket: bucket,
		Key:    key,
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			default:
				http.Error(w, fmt.Sprintf("Failed to upload data to %s/%s, %s: %s\n", *bucket, *key, aerr.Code(), aerr.Message()), 500)
			}
		} else {
			fmt.Println(err.Error())
		}
		return
	}

	allObjects, err := s3Client.ListObjectsV2(&s3.ListObjectsV2Input{
		Bucket:  bucket,
		MaxKeys: aws.Int64(1000),
	})
	fmt.Fprintf(w, "%v", allObjects)
}

func main() {
	config, err := LoadConfig(".")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/s3", config.s3Handler)
	loggingMux := NewLogger(mux)

	fmt.Println("HTTP Server Starting...")
	err = http.ListenAndServe(fmt.Sprintf(":%v", config.HttpPort), loggingMux)
	if err != nil {
		fmt.Println(err)
	}

}
