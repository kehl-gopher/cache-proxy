package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	origin    string
	port      int
	cacheLock sync.RWMutex
	red       *redis.Client
)

func main() {

	originUrl := flag.CommandLine.String("origin", "", "user origin url")
	portFlag := flag.CommandLine.Int("port", 0, "user origin port ")
	flag.Parse()

	if originUrl == nil || *originUrl == "" {
		panic("origin url is required")
	}

	if portFlag == nil || *portFlag == 0 {
		panic("port is required")
	}

	origin = *originUrl
	port = *portFlag

	fmt.Printf("port %d originUrl %s", port, origin)
	red = redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	serveMux := http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		ReadTimeout:  time.Minute * 10,
		WriteTimeout: time.Minute * 10,
		IdleTimeout:  time.Minute * 30,
	}

	// handle grace ful shutdown for server shutdown

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Kill, os.Interrupt, syscall.SIGTERM)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	go func() {
		err := serveMux.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			log.Fatalln("Server error:", err)
		}
	}()

	<-quit
	log.Println("Shutting down serve...")

	if err := serveMux.Shutdown(ctx); err != nil {
		log.Println("err shutting down server", err)
	} else {
		log.Println("shutdown server")
	}
}
