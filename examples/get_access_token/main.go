package main

// go run examples/get_access_token.go <client_id> <device_code> [repo_id]

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
	if len(os.Args) < 3 {
		return errors.New("client ID and device code are required")
	}
	clientID := os.Args[1]
	deviceCode := os.Args[2]
	repoID := ""
	if len(os.Args) == 4 {
		repoID = os.Args[3]
	}
	resp, _, _, err := client.GetAccessToken(ctx, clientID, deviceCode, &deviceflow.InputGetAccessToken{
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
