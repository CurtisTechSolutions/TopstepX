// Command realtime-user streams account, order, position and trade updates
// from the user hub until interrupted. It is read-only.
//
//	TOPSTEPX_USERNAME=... TOPSTEPX_API_KEY=... go run ./examples/realtime-user
//
// TOPSTEPX_API_URL and TOPSTEPX_USER_HUB_URL override the default URLs.
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
	accountID := flag.Int("account", 0, "account to watch (default: the first active account)")
	flag.Parse()

	client, err := newClient()
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	id := int32(*accountID)
	if id == 0 {
		accounts, err := client.Accounts.Search(ctx, true)
		if err != nil {
			log.Fatal(err)
		}
		if len(accounts) == 0 {
			log.Fatal("no active account")
		}
		id = accounts[0].ID
	}

	hub := realtime.NewUserHubForClient(client)
	defer hub.Close()
	if err := hub.Connect(ctx); err != nil {
		log.Fatal(err)
	}
	subscribe := []func() error{
		func() error { return hub.SubscribeAccounts(ctx) },
		func() error { return hub.SubscribeOrders(ctx, id) },
		func() error { return hub.SubscribePositions(ctx, id) },
		func() error { return hub.SubscribeTrades(ctx, id) },
	}
	for _, s := range subscribe {
		if err := s(); err != nil {
			log.Fatal(err)
		}
	}
	fmt.Printf("watching account %d; press Ctrl-C to stop\n", id)

	go func() {
		<-ctx.Done()
		hub.Close()
	}()
	for ev := range hub.Events() {
		switch ev := ev.(type) {
		case *realtime.AccountEvent:
			fmt.Printf("account %d balance=%v canTrade=%v\n", ev.Account.ID, ev.Account.Balance, deref(ev.Account.CanTrade))
		case *realtime.OrderEvent:
			fmt.Printf("order %d status=%v size=%v filled=%v\n", ev.Order.ID, deref(ev.Order.Status), deref(ev.Order.Size), deref(ev.Order.FillVolume))
		case *realtime.PositionEvent:
			fmt.Printf("position %s type=%v size=%v avg=%v\n", ev.Position.ContractID, deref(ev.Position.Type), deref(ev.Position.Size), ev.Position.AveragePrice)
		case *realtime.UserTradeEvent:
			fmt.Printf("trade %d price=%v pnl=%v\n", ev.Trade.ID, ev.Trade.Price, ev.Trade.ProfitAndLoss)
		case *realtime.StateEvent:
			fmt.Printf("state %s attempt=%d err=%v\n", ev.State, ev.Attempt, ev.Err)
		case *realtime.SubscriptionErrorEvent:
			fmt.Printf("resubscribe %s failed: %v\n", ev.Method, ev.Err)
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
	if u := os.Getenv("TOPSTEPX_USER_HUB_URL"); u != "" {
		opts = append(opts, topstepx.WithUserHubURL(u))
	}
	return topstepx.NewClient(opts...)
}
