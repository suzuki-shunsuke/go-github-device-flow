package main

// go run examples/get_device_code.go <client_id>

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/suzuki-shunsuke/go-github-device-flow/deviceflow"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	client := deviceflow.New(&deviceflow.Input{})
	if len(os.Args) < 2 {
		return errors.New("client ID is required")
	}
	clientID := os.Args[1]
	resp, _, _, err := client.GetDeviceCode(ctx, clientID)
	if resp != nil {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(resp); err != nil {
			return err
		}
	}
	if err != nil {
		return err
	}
	return nil
}
