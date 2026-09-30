// Command realtime-market streams quotes, market trades and market depth for a
// contract until interrupted. It is read-only.
//
//	TOPSTEPX_USERNAME=... TOPSTEPX_API_KEY=... go run ./examples/realtime-market -q ES
//
// TOPSTEPX_API_URL and TOPSTEPX_MARKET_HUB_URL override the default URLs.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	topstepx "github.com/CurtisTechSolutions/TopstepX/go"
	"github.com/CurtisTechSolutions/TopstepX/go/realtime"
)

func main() {
	contractID := flag.String("contract", "", "contract identifier (default: first match of -q)")
	query := flag.String("q", "ES", "contract search text used when -contract is empty")
	depth := flag.Bool("depth", false, "also subscribe to market depth")
	flag.Parse()

	client, err := newClient()
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	id := *contractID
	if id == "" {
		contracts, err := client.Contracts.Search(ctx, *query, false)
		if err != nil {
			log.Fatal(err)
		}
		if len(contracts) == 0 {
			log.Fatalf("no contract matches %q", *query)
		}
		id = contracts[0].ID
	}

	hub := realtime.NewMarketHubForClient(client)
	defer hub.Close()
	if err := hub.Connect(ctx); err != nil {
		log.Fatal(err)
	}
	if err := hub.SubscribeContractQuotes(ctx, id); err != nil {
		log.Fatal(err)
	}
	if err := hub.SubscribeContractTrades(ctx, id); err != nil {
		log.Fatal(err)
	}
	if *depth {
		if err := hub.SubscribeContractMarketDepth(ctx, id); err != nil {
			log.Fatal(err)
		}
	}
	fmt.Printf("streaming %s; press Ctrl-C to stop\n", id)

	go func() {
		<-ctx.Done()
		hub.Close()
	}()
	for ev := range hub.Events() {
		switch ev := ev.(type) {
		case *realtime.QuoteEvent:
			fmt.Printf("quote %s last=%v bid=%v ask=%v\n", ev.ContractID, ev.Quote.LastPrice, ev.Quote.BestBid, ev.Quote.BestAsk)
		case *realtime.MarketTradesEvent:
			for _, t := range ev.Trades {
				fmt.Printf("trade %s price=%v volume=%v\n", ev.ContractID, t.Price, deref(t.Volume))
			}
		case *realtime.DepthEvent:
			fmt.Printf("depth %s: %d entries\n", ev.ContractID, len(ev.Entries))
		case *realtime.StateEvent:
			fmt.Printf("state %s attempt=%d err=%v\n", ev.State, ev.Attempt, ev.Err)
		case *realtime.UnknownEvent:
			fmt.Printf("unknown %s: %v\n", ev.Target, ev.Err)
		}
	}
	if n := hub.Dropped(); n > 0 {
		fmt.Printf("%d events were dropped\n", n)
	}
}

func deref[T any](p *T) any {
	if p == nil {
		return "-"
	}
	return *p
}

// newClient builds a client from TOPSTEPX_USERNAME, TOPSTEPX_API_KEY and the
// optional URL overrides.
func newClient() (*topstepx.Client, error) {
	user, key := os.Getenv("TOPSTEPX_USERNAME"), os.Getenv("TOPSTEPX_API_KEY")
	if user == "" || key == "" {
		return nil, errors.New("set TOPSTEPX_USERNAME and TOPSTEPX_API_KEY")
	}
	opts := []topstepx.Option{topstepx.WithCredentials(user, key)}
	if u := os.Getenv("TOPSTEPX_API_URL"); u != "" {
		opts = append(opts, topstepx.WithBaseURL(u))
	}
	if u := os.Getenv("TOPSTEPX_MARKET_HUB_URL"); u != "" {
		opts = append(opts, topstepx.WithMarketHubURL(u))
	}
	return topstepx.NewClient(opts...)
}
