package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
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

type Result struct {
	StatusCode int                    `json:"status_code,omitempty"`
	Data       map[string]interface{} `json:"data,omitempty"`
	Message    string                 `json:"message,omitempty"`
	Error      error                  `json:"error,omitempty"`
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

	cacheLock = &sync.RWMutex{}

	flag.CommandLine.StringVar(&cacheArgs.origin, "origin", "", "origin flags")
	flag.CommandLine.IntVar(&cacheArgs.port, "port", 0, "port flags")
	flag.CommandLine.IntVar(&cacheArgs.maxAge, "max-age", 60*60*24, "max age flags") // default to 24 hours

	flag.Parse()
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

	// define route
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

func sendRequest(url string, resp chan<- interface{}, r *http.Request) (int, error) {
	url += r.URL.Path

	res, err := http.Get(url)
	if err != nil {
		return res.StatusCode, err
	}

	b, _ := io.ReadAll(res.Body)
	resp <- b
	return res.StatusCode, nil
}

func hashKey(origin string) string {

	u, err := url.Parse(origin)

	if err != nil {
		panic(err)
	}

	if u.Path != "" || u.Path == "/" {
		err := errors.New("origin url must not contain path eg dummyjson.com/ is invalid or dummyjson.com/abc are invalid")
		panic(err)
	}

	key := fmt.Sprintf("cache-request/GET/%s", origin)
	sha := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%x", sha[:])
}

func proxyHandler(w http.ResponseWriter, r *http.Request) {
	key := hashKey(cacheArgs.origin)

	var data = make(map[string]interface{})

	resp := make(chan interface{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*4)
	defer cancel()
	res, err := red.Get(ctx, key).Result()

	if err != nil && err != redis.Nil {
		reslt := Result{Message: "internal server error", StatusCode: http.StatusInternalServerError, Error: err}
		writeResponse(w, reslt, reslt.StatusCode)
		return
	} else if res == "" || err == redis.Nil {
		utils.PrintLogs(logs, utils.InfoLevel, "Cache miss: data not found in cache storage ", "sending request to origin server")
		var statusCode int
		var err error
		go func() {
			statusCode, err = sendRequest(cacheArgs.origin, resp, r)
		}()

		if err != nil {
			utils.PrintLogs(logs, utils.ErrorLevel, err, fmt.Sprintf("status_code=%d", statusCode))
			json.NewEncoder(w).Encode(res)
			return
		}
		dataResp := <-resp

		cacheLock.Lock()

		err = red.SetEx(ctx, key, dataResp, time.Second*time.Duration(cacheArgs.maxAge)).Err()
		if err != nil {
			reslt := Result{Message: "internal server error", StatusCode: http.StatusInternalServerError, Error: err}
			writeResponse(w, reslt, statusCode)
			return
		}

		err = json.Unmarshal(dataResp.([]byte), &data)
		reslt := Result{StatusCode: http.StatusOK, Data: data, Message: "Cache miss"}

		maxAge := strconv.FormatInt(time.Now().Add(time.Second*time.Duration(cacheArgs.maxAge)).Unix(), 10)
		w.Header().Add("X-Cache", "Miss")
		w.Header().Add("max-age", maxAge)

		writeResponse(w, reslt, statusCode)

		cacheLock.Unlock()
		return
	}

	err = json.Unmarshal([]byte(res), &data)
	reslt := Result{StatusCode: http.StatusOK, Data: data, Message: "Cache Hit"}

	maxAge := strconv.FormatInt(time.Now().Add(time.Second*time.Duration(cacheArgs.maxAge)).Unix(), 10)
	w.Header().Add("X-Cache", "Hit")
	w.Header().Add("max-age", maxAge)
	writeResponse(w, reslt, http.StatusOK)
}

func writeResponse(w http.ResponseWriter, data interface{}, statusCode int) {
	w.WriteHeader(statusCode)
	w.Header().Add("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)

}
