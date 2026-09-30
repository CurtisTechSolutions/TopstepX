// Command accounts lists the active trading accounts. It is read-only.
//
//	TOPSTEPX_USERNAME=... TOPSTEPX_API_KEY=... go run ./examples/accounts
//
// TOPSTEPX_API_URL overrides the REST base URL.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	topstepx "github.com/CurtisTechSolutions/TopstepX/go"
)

func main() {
	client, err := newClient()
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	accounts, err := client.Accounts.Search(ctx, true)
	if err != nil {
		var apiErr *topstepx.APIError
		if errors.As(err, &apiErr) {
			log.Fatalf("gateway rejected the request: %s (code %d)", apiErr.CodeName, apiErr.Code)
		}
		log.Fatal(err)
	}
	for _, a := range accounts {
		fmt.Printf("%d\t%s\tbalance=%s\tcanTrade=%t\tsimulated=%t\n", a.ID, a.Name, a.Balance, a.CanTrade, a.Simulated)
	}
}

// newClient builds a client from TOPSTEPX_USERNAME, TOPSTEPX_API_KEY and the
// optional TOPSTEPX_API_URL.
func newClient() (*topstepx.Client, error) {
	user, key := os.Getenv("TOPSTEPX_USERNAME"), os.Getenv("TOPSTEPX_API_KEY")
	if user == "" || key == "" {
		return nil, errors.New("set TOPSTEPX_USERNAME and TOPSTEPX_API_KEY")
	}
	opts := []topstepx.Option{topstepx.WithCredentials(user, key)}
	if u := os.Getenv("TOPSTEPX_API_URL"); u != "" {
		opts = append(opts, topstepx.WithBaseURL(u))
	}
	return topstepx.NewClient(opts...)
}
