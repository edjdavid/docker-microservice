package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/spf13/viper"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Config struct {
	MONGO_USER     string
	MONGO_PASSWORD string
	MONGO_DSN      string
	HTTP_PORT      int
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

func main() {
	config, err := LoadConfig(".")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	// none of these handlers are currently restful, GET request will update their respective stores
	mux.HandleFunc("/mongo", config.mongoHandler)
	loggingMux := NewLogger(mux)

	fmt.Println("HTTP Server Starting...")
	err = http.ListenAndServe(fmt.Sprintf(":%v", config.HTTP_PORT), loggingMux)
	if err != nil {
		fmt.Println(err)
	}

}
