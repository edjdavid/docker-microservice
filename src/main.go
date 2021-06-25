package main

import (
	"context"
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
	"github.com/go-redis/redis/v8"
	"github.com/spf13/viper"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Config struct {
	MONGO_USER         string
	MONGO_PASSWORD     string
	MONGO_DSN          string
	REDIS_DSN          string
	S3_ENDPOINT        string
	S3_DISABLE_SSL     bool
	S3_FORCE_PATHSTYLE bool
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

// Driver docs https://github.com/mongodb/mongo-go-driver
func (config *Config) mongoHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	credential := options.Credential{
		Username: config.MONGO_USER,
		Password: config.MONGO_PASSWORD,
	}
	clientOpts := options.Client().
		ApplyURI(config.MONGO_DSN).
		SetAuth(credential)
	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		http.Error(w, fmt.Sprint(err), 500)
		return
	}

	defer func() {
		if err = client.Disconnect(ctx); err != nil {
			http.Error(w, fmt.Sprint(err), 500)
			return
		}
	}()

	collection := client.Database("local").Collection("demo")

	collection.InsertOne(ctx, bson.M{
		"host": r.Host,
		"url":  r.URL,
		"time": time.Now().Format(time.RFC3339),
	})

	cur, err := collection.Find(ctx, bson.D{})
	if err != nil {
		http.Error(w, fmt.Sprint(err), 500)
		return
	}
	defer cur.Close(ctx)

	for cur.Next(ctx) {
		var result bson.D
		err := cur.Decode(&result)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Fprintln(w, result)
	}

	if err := cur.Err(); err != nil {
		http.Error(w, fmt.Sprint(err), 500)
		return
	}
}

// Driver docs https://github.com/go-redis/redis
func (config *Config) redisHandler(w http.ResponseWriter, r *http.Request) {
	opt, err := redis.ParseURL(config.REDIS_DSN)
	if err != nil {
		fmt.Println(opt)
		http.Error(w, fmt.Sprint(err), 500)
		return
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:     opt.Addr,
		Password: opt.Password,
		DB:       opt.DB,
	})
	defer rdb.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	urlQuery := r.URL.Query()
	if urlQuery.Get("key") != "" && urlQuery.Get("value") != "" {
		err = rdb.Set(ctx, urlQuery.Get("key"), urlQuery.Get("value"), 0).Err()

	} else {
		val, _ := rdb.Get(ctx, "foo").Int64()
		err = rdb.Set(ctx, "foo", val+1, 0).Err()
	}
	if err != nil {
		http.Error(w, fmt.Sprint(err), 500)
		return
	}

	iter := rdb.Scan(ctx, 0, "*", 0).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		fmt.Fprintln(w, iter.Val(), rdb.Get(ctx, key).Val())
	}
	if err := iter.Err(); err != nil {
		http.Error(w, fmt.Sprint(err), 500)
		return
	}
}

// Driver docs https://docs.aws.amazon.com/sdk-for-go/v1/developer-guide/using-s3-with-go-sdk.html
func (config *Config) s3Handler(w http.ResponseWriter, r *http.Request) {
	s3Config := &aws.Config{
		Endpoint:         aws.String(config.S3_ENDPOINT),
		DisableSSL:       aws.Bool(config.S3_DISABLE_SSL),
		S3ForcePathStyle: aws.Bool(config.S3_FORCE_PATHSTYLE),
	}

	sess := session.New(s3Config)
	s3Client := s3.New(sess)

	bucket := aws.String("demo-bucket")
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

func helloHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Hello<br> - Go HTTP Server")
}

func main() {
	config, err := LoadConfig(".")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	// none of these handlers are currently restful, GET request will update their respective stores
	mux.HandleFunc("/mongo", config.mongoHandler)
	mux.HandleFunc("/s3", config.s3Handler)
	mux.HandleFunc("/redis", config.redisHandler)
	mux.HandleFunc("/hello", helloHandler)
	loggingMux := NewLogger(mux)

	fmt.Println("HTTP Server Starting...")
	err = http.ListenAndServe(":80", loggingMux)
	if err != nil {
		fmt.Println(err)
	}

}
