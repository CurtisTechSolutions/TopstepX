// Command contracts searches contracts and prints recent bars of the first
// match. It is read-only.
//
//	TOPSTEPX_USERNAME=... TOPSTEPX_API_KEY=... go run ./examples/contracts -q ES
//
// TOPSTEPX_API_URL overrides the REST base URL.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	topstepx "github.com/CurtisTechSolutions/TopstepX/go"
)

func main() {
	query := flag.String("q", "ES", "contract search text")
	live := flag.Bool("live", false, "use the live data subscription instead of the simulated one")
	bars := flag.Int("bars", 20, "number of 5-minute bars to print (at most 20000)")
	flag.Parse()

	client, err := newClient()
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	contracts, err := client.Contracts.Search(ctx, *query, *live)
	if err != nil {
		log.Fatal(err)
	}
	if len(contracts) == 0 {
		log.Fatalf("no contract matches %q", *query)
	}
	for _, c := range contracts {
		fmt.Printf("%s\t%s\t%s\ttick %s = %s\tactive=%t\n", c.ID, c.Name, c.Description, c.TickSize, c.TickValue, c.ActiveContract)
	}

	contract := contracts[0]
	end := time.Now().UTC()
	history, err := client.History.RetrieveBars(ctx, topstepx.RetrieveBarRequest{
		ContractID: contract.ID,
		Live:       *live,
		StartTime:  end.Add(-7 * 24 * time.Hour),
		EndTime:    end,
		Unit:       topstepx.AggregateBarUnitMinute,
		UnitNumber: 5,
		Limit:      int32(*bars),
	})
	if err != nil {
		var ve *topstepx.ValidationError
		if errors.As(err, &ve) {
			log.Fatalf("invalid request: %s", ve.Reason)
		}
		log.Fatal(err)
	}
	fmt.Printf("\n%d bars of %s:\n", len(history), contract.Name)
	for _, b := range history {
		fmt.Printf("%s\to=%s h=%s l=%s c=%s v=%d\n", b.Time.Format(time.RFC3339), b.Open, b.High, b.Low, b.Close, b.Volume)
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
