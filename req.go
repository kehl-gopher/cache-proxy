package main

import (
	"encoding/json"
	"io"
	"net/http"
)

type Response struct {
	StatusCode int
	Body       []byte
	Error      error
}

func sendRequest(url string, resp chan<- Response, r *http.Request) {
	url += r.URL.Path

	res, err := http.Get(url)
	if err != nil {
		resp <- Response{StatusCode: res.StatusCode, Body: nil, Error: err}
		return
	}

	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	resp <- Response{StatusCode: res.StatusCode, Body: b, Error: nil}

}

func writeResponse(w http.ResponseWriter, data interface{}, statusCode int) {
	w.WriteHeader(statusCode)
	w.Header().Add("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}
