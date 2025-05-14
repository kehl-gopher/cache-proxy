package main

import (
	"encoding/json"
	"io"
	"net/http"
)

func sendRequest(url string, resp chan<- interface{}, r *http.Request) (int, error) {

	url += r.URL.Path

	var data map[string]interface{}

	res, err := http.Get(url)
	if err != nil {
		return res.StatusCode, err
	}

	b, _ := io.ReadAll(res.Body)

	err = json.Unmarshal(b, &data)

	if err != nil {
		return 0, err
	}
	resp <- data
	return res.StatusCode, nil
}

func writeResponse(w http.ResponseWriter, data interface{}, statusCode int) {
	w.WriteHeader(statusCode)
	w.Header().Add("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}
