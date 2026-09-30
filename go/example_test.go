package topstepx_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	topstepx "github.com/CurtisTechSolutions/TopstepX/go"
	"github.com/shopspring/decimal"
)

func ExampleNewClient() {
	client, err := topstepx.NewClient(
		topstepx.WithCredentials(os.Getenv("TOPSTEPX_USERNAME"), os.Getenv("TOPSTEPX_API_KEY")),
	)
	if err != nil {
		log.Fatal(err)
	}
	accounts, err := client.Accounts.Search(context.Background(), true) // logs in lazily
	if err != nil {
		log.Fatal(err)
	}
	for _, a := range accounts {
		fmt.Println(a.ID, a.Name, a.Balance)
	}
}

func ExampleOrdersService_Place() {
	client, _ := topstepx.NewClient(topstepx.WithCredentials("user", "api-key"))
	req := topstepx.NewLimitOrder(123, "CON.F.US.EP.U25", topstepx.OrderSideBid, 1, decimal.RequireFromString("6000.25")).
		WithStopLossBracket(8, topstepx.OrderTypeStop).
		WithTakeProfitBracket(16, topstepx.OrderTypeLimit)
	id, err := client.Orders.Place(context.Background(), req)
	var apiErr *topstepx.APIError
	if errors.As(err, &apiErr) {
		// A rejected order can still carry an identifier.
		fmt.Println("rejected:", apiErr.CodeName, apiErr.OrderID)
		return
	}
	fmt.Println("placed", id, err)
}

func ExampleClient_Do() {
	client, _ := topstepx.NewClient(topstepx.WithToken("token"))
	var resp topstepx.PlaceOrderResponse
	err := client.Do(context.Background(), topstepx.OpOrderPlaceOrder,
		topstepx.NewMarketOrder(123, "CON.F.US.EP.U25", topstepx.OrderSideAsk, 1), &resp)
	if err != nil {
		log.Fatal(err) // HTTP, transport, decode or validation error
	}
	if resp.IsBusinessFailure() {
		fmt.Println("rejected with code", resp.ErrorCode, "orderId", resp.OrderID)
	}
}
