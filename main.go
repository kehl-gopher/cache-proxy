package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/kehl-gopher/cache-proxy/utils"
	"github.com/redis/go-redis/v9"
)

var (
	cacheLock *sync.RWMutex
	red       *redis.Client
	logs      *utils.Logs
	cacheArgs CacheFlags
)

type CacheFlags struct {
	maxAge int
	origin string
	port   int
}

func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				utils.PrintLogs(logs, utils.ErrorLevel, "panic error", fmt.Errorf("%v", err))
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func main() {

	logs = utils.NewLogs()

	flag.Parse()
	flag.StringVar(&cacheArgs.origin, "origin", "", "origin flags")
	flag.IntVar(&cacheArgs.port, "port", 0, "port flags")
	flag.IntVar(&cacheArgs.maxAge, "max-age", 24, "max age flags")

	if cacheArgs.origin == "" {
		panic("origin cannot be missing")
	}

	if cacheArgs.port == 0 {
		panic("port server port cannot be missing")
	}

	red = redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	_, err := red.Ping(context.Background()).Result()
	if err != nil {
		utils.PrintLogs(logs, utils.FatalLevel, "unable to ping redis server", err)
	}

	serveMux := http.Server{
		Addr:         fmt.Sprintf(":%d", cacheArgs.port),
		ReadTimeout:  time.Minute * 10,
		WriteTimeout: time.Minute * 10,
		IdleTimeout:  time.Minute * 30,
	}

	utils.PrintLogs(logs, utils.InfoLevel, fmt.Sprintf("proxy server start on port: %d - forward address on %s", cacheArgs.port, cacheArgs.origin), "")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Kill, os.Interrupt, syscall.SIGTERM)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	go func() {
		err := serveMux.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			utils.PrintLogs(logs, utils.FatalLevel, "unknown server error occured while shutting down", err)
		}
	}()

	<-quit
	utils.PrintLogs(logs, utils.InfoLevel, "shutting down server", "")

	if err := serveMux.Shutdown(ctx); err != nil {
		utils.PrintLogs(logs, utils.ErrorLevel, "server shutdown error", err)
	} else {
		utils.PrintLogs(logs, utils.InfoLevel, "Server shutdown successful")
	}
}

func sendRequest(url string, resp chan<- interface{}) {

}

func createKey(val string) string {
	return ""
}

func proxyHandler(w http.ResponseWriter, r *http.Request) {

}
