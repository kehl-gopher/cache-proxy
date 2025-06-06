package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kehl-gopher/cache-proxy/utils"
	"github.com/nitishm/go-rejson/v4"
)

var (
	red        *redis.Client
	logs       *utils.Logs
	cacheArgs  CacheFlags
	rh         *rejson.Handler
	redisQueue = make(chan CacheDataItem, 100)
)

type CacheFlags struct {
	maxAge     int
	origin     string
	port       int
	clearCache bool
}

type CacheDataItem struct {
	Key   string
	Value json.RawMessage
	TTL   int
}

type Result struct {
	StatusCode int         `json:"status_code,omitempty"`
	Data       interface{} `json:"data,omitempty"`
	Message    string      `json:"message,omitempty"`
	Error      error       `json:"error,omitempty"`
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

	go startRedisWorker() // start redis queue server
	defer close(redisQueue)
	logs = utils.NewLogs()

	flag.CommandLine.StringVar(&cacheArgs.origin, "origin", "", "origin flags")
	flag.CommandLine.IntVar(&cacheArgs.port, "port", 0, "port flags")
	flag.CommandLine.IntVar(&cacheArgs.maxAge, "max-age", 60*60*24, "max age flags") // default to 24 hours
	flag.CommandLine.BoolVar(&cacheArgs.clearCache, "clear-cache", false, "clear cachae flag")

	flag.Parse()
	if cacheArgs.origin == "" && cacheArgs.clearCache == false {
		panic("origin cannot be missing")
	}

	if cacheArgs.port == 0 && cacheArgs.clearCache == false {
		panic("port server port cannot be missing")
	}

	red = redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	defer func() {
		if err := red.Close(); err != nil {
			utils.PrintLogs(logs, utils.FatalLevel, "could not connect to redis server")
		}
	}()

	rh = rejson.NewReJSONHandler()

	rh.SetGoRedisClientWithContext(context.Background(), red)

	if cacheArgs.clearCache {
		err := red.FlushDBAsync(context.Background()).Err()
		if err != nil {
			utils.PrintLogs(logs, utils.ErrorLevel, err, "unable to clear cache")
			return
		}

		utils.PrintLogs(logs, utils.InfoLevel, "cache cleared succesfully")
		return
	}
	serveMux := http.Server{
		Addr:         fmt.Sprintf(":%d", cacheArgs.port),
		ReadTimeout:  time.Minute * 10,
		WriteTimeout: time.Minute * 10,
		IdleTimeout:  time.Minute * 30,
	}

	// dynamically handle any routes...
	http.Handle("GET /", recoveryMiddleware(http.HandlerFunc(proxyHandler))) // first time this nigga is useful and helpful

	utils.PrintLogs(logs, utils.InfoLevel,
		fmt.Sprintf("proxy server start on port: %d - forward address on %s", cacheArgs.port, cacheArgs.origin), "")
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Kill, os.Interrupt, syscall.SIGTERM)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	go func() {
		err := serveMux.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			utils.PrintLogs(logs, utils.FatalLevel, "server error while shutting down ", err)
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

func hashKey(origin string, path string) string {

	u, err := url.Parse(origin)

	if err != nil {
		panic(err)
	}

	if u.Path != "" || u.Path == "/" {
		err := errors.New("origin url must not contain path eg dummyjson.com/ is invalid or dummyjson.com/abc are invalid")
		panic(err)
	}

	key := fmt.Sprintf("cache-request/GET/%s+%s", origin, path)
	sha := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%x", sha[:])
}

func proxyHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*100)
	defer cancel()

	var data json.RawMessage
	key := hashKey(cacheArgs.origin, r.URL.Path)
	de, err := getJSON(ctx, key, &data)

	if err != nil && err != redis.Nil {
		utils.PrintLogs(logs, utils.FatalLevel, fmt.Sprintf("Unexpected server error while fetch data in redis %s", err.Error()))
		return
	}

	var resp = make(chan Response)
	if err == redis.Nil && de == "" {
		utils.PrintLogs(logs, utils.InfoLevel, "Cache miss: data not found in cache storage")
		go sendRequest(cacheArgs.origin, resp, r)
		dataResp := <-resp
		err := json.Unmarshal(dataResp.Body, &data)
		if err != nil {
			utils.PrintLogs(logs, utils.FatalLevel, fmt.Sprintf("unexpected error in server level-->"))
			writeResponse(w, dataResp, dataResp.StatusCode)
		}
		redisQueue <- CacheDataItem{Key: key, Value: data, TTL: cacheArgs.maxAge}
		w.Header().Add("X-Cache", "MISS")
		w.Header().Add("Cache-Control", fmt.Sprintf("max-age=%d", time.UnixMilli(int64(cacheArgs.maxAge)).Unix()))
		writeResponse(w, data, dataResp.StatusCode)
		return
	}

	w.Header().Add("X-Cache", "HIT")
	w.Header().Add("Cache-Control", fmt.Sprintf("max-age=%d", time.UnixMilli(int64(cacheArgs.maxAge)).Unix()))
	writeResponse(w, data, http.StatusOK)
}

func startRedisWorker() {
	go func() {
		for queue := range redisQueue {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := setJSON(ctx, queue.Key, queue.Value, queue.TTL)
			cancel()
			if err != nil {
				utils.PrintLogs(logs, utils.InfoLevel, "failed to write to redis "+err.Error(), err)
				continue
			}
		}
	}()

}
