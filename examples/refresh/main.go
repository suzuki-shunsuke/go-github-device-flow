package main

// go run examples/refresh_token.go <client_id> <refresh_token>

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	if len(os.Args) < 3 {
		return errors.New("client ID and refresh token are required")
	}
	clientID := os.Args[1]
	refreshToken := os.Args[2]
	resp, _, body, err := client.RefreshToken(ctx, clientID, refreshToken)
	if resp != nil {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(resp); err != nil {
			return err
		}
	}
	if body != nil {
		fmt.Println(string(body))
	}
	if err != nil {
		return err
	}
	return nil
}
