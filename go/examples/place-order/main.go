// Command place-order places a limit order and cancels it again.
//
// ORDERS ARE REAL. The command refuses to run unless TOPSTEPX_ALLOW_TRADING=1
// is set, and refuses accounts that are not simulated unless -allow-live is
// given. Use a simulated account while developing.
//
//	TOPSTEPX_ALLOW_TRADING=1 TOPSTEPX_USERNAME=... TOPSTEPX_API_KEY=... \
//	  go run ./examples/place-order -account 123 -contract CON.F.US.EP.U25 -price 5000
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
	"github.com/shopspring/decimal"
)

func main() {
	accountID := flag.Int("account", 0, "account identifier (required)")
	contractID := flag.String("contract", "", "contract identifier (required)")
	price := flag.String("price", "", "limit price, far from the market (required)")
	sell := flag.Bool("sell", false, "sell instead of buy")
	size := flag.Int("size", 1, "order size")
	hold := flag.Duration("hold", 5*time.Second, "how long to leave the order working before cancelling it")
	allowLive := flag.Bool("allow-live", false, "allow an account that is not simulated")
	flag.Parse()

	if os.Getenv("TOPSTEPX_ALLOW_TRADING") != "1" {
		log.Fatal("refusing to place an order: orders are real. Set TOPSTEPX_ALLOW_TRADING=1 to confirm, and use a simulated account.")
	}
	if *accountID == 0 || *contractID == "" || *price == "" {
		flag.Usage()
		os.Exit(2)
	}
	limit, err := decimal.NewFromString(*price)
	if err != nil {
		log.Fatalf("invalid -price: %v", err)
	}

	client, err := newClient()
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute+*hold)
	defer cancel()

	accounts, err := client.Accounts.Search(ctx, true)
	if err != nil {
		log.Fatal(err)
	}
	var account *topstepx.TradingAccount
	for i := range accounts {
		if accounts[i].ID == int32(*accountID) {
			account = &accounts[i]
		}
	}
	switch {
	case account == nil:
		log.Fatalf("account %d is not an active account of this user", *accountID)
	case !account.CanTrade:
		log.Fatalf("account %d cannot trade", *accountID)
	case !account.Simulated && !*allowLive:
		log.Fatalf("account %d is not simulated; pass -allow-live to trade it anyway", *accountID)
	}

	side := topstepx.OrderSideBid
	if *sell {
		side = topstepx.OrderSideAsk
	}
	req := topstepx.NewLimitOrder(account.ID, *contractID, side, int32(*size), limit).
		WithCustomTag(fmt.Sprintf("sdk-example-%d", time.Now().UnixNano()))
	orderID, err := client.Orders.Place(ctx, req)
	if err != nil {
		var apiErr *topstepx.APIError
		if errors.As(err, &apiErr) {
			// A rejected order can still carry an identifier.
			log.Fatalf("order rejected: %s (code %d, orderId %v): %v", apiErr.CodeName, apiErr.Code, apiErr.OrderID, err)
		}
		log.Fatal(err)
	}
	fmt.Printf("placed order %d; cancelling in %s\n", orderID, *hold)

	time.Sleep(*hold)
	if err := client.Orders.Cancel(ctx, account.ID, orderID); err != nil {
		log.Fatalf("cancel order %d: %v", orderID, err)
	}
	fmt.Printf("cancelled order %d\n", orderID)
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
