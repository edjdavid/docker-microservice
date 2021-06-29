package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/spf13/viper"
)

type Config struct {
	RedisDsn string `mapstructure:"REDIS_DSN"`
	HttpPort int    `mapstructure:"HTTP_PORT"`
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

// Driver docs https://github.com/go-redis/redis
func (config *Config) redisHandler(w http.ResponseWriter, r *http.Request) {
	opt, err := redis.ParseURL(config.RedisDsn)
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

func main() {
	config, err := LoadConfig(".")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	// none of these handlers are currently restful, GET request will update their respective stores
	mux.HandleFunc("/redis", config.redisHandler)
	loggingMux := NewLogger(mux)

	fmt.Println("HTTP Server Starting...")
	err = http.ListenAndServe(fmt.Sprintf(":%v", config.HttpPort), loggingMux)
	if err != nil {
		fmt.Println(err)
	}

}
