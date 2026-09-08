package main

// go run examples/poll.go <client_id> [repo_id]

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
	repoID := ""
	if len(os.Args) == 3 {
		repoID = os.Args[2]
	}

	deviceCodeResp, _, _, err := client.GetDeviceCode(ctx, clientID)
	if deviceCodeResp != nil {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(deviceCodeResp); err != nil {
			return err
		}
	}
	if err != nil {
		return err
	}

	resp, err := client.Poll(ctx, nil, clientID, deviceCodeResp, &deviceflow.InputGetAccessToken{
		RepositoryID: repoID,
	})
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
