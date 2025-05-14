package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/nitishm/go-rejson/v4/rjs"
)

func setJSON(ctx context.Context, key string, data interface{}, maxAge int) error {

	res, err := rh.JSONSet(key, ".", data, rjs.SetOptionNX)
	if err != nil {
		return err
	}
	if res.(string) != "OK" {
		return errors.New("failed to set json value")
	}

	err = red.Expire(ctx, key, time.Second*time.Duration(maxAge)).Err()

	if err != nil {
		return err
	}
	return nil
}

func getJSON(ctx context.Context, key string, data interface{}) (string, error) {
	res, err := red.Do(ctx, "JSON.GET", key, "$").Result()

	if err != nil {
		return "", err
	}
	err = json.Unmarshal([]byte(res.(string)), data)
	if err != nil {
		return "", err
	}
	return res.(string), nil
}
