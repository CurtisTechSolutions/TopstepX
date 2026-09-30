package realtime_test

import (
	"context"
	"fmt"
	"log"

	topstepx "github.com/CurtisTechSolutions/TopstepX/go"
	"github.com/CurtisTechSolutions/TopstepX/go/realtime"
)

func ExampleNewUserHubForClient() {
	client, err := topstepx.NewClient(topstepx.WithCredentials("user", "api-key"))
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	hub := realtime.NewUserHubForClient(client)
	if err := hub.Connect(ctx); err != nil {
		log.Fatal(err)
	}
	defer hub.Close()
	if err := hub.SubscribeOrders(ctx, 123); err != nil {
		log.Fatal(err)
	}
	for ev := range hub.Events() {
		switch ev := ev.(type) {
		case *realtime.OrderEvent:
			fmt.Println("order", ev.Order.ID, ev.Order.Status)
		case *realtime.StateEvent:
			fmt.Println("state", ev.State, ev.Err)
		}
	}
}

func ExampleNewMarketHub() {
	client, _ := topstepx.NewClient(topstepx.WithCredentials("user", "api-key"))
	hub := realtime.NewMarketHub(client.Token,
		realtime.WithURL(client.MarketHubURL()),
		realtime.WithEventBuffer(4096),
		realtime.WithOverflowPolicy(realtime.DropOldest),
	)
	if err := hub.Connect(context.Background()); err != nil {
		log.Fatal(err)
	}
	defer hub.Close()
	_ = hub.SubscribeContractQuotes(context.Background(), "CON.F.US.EP.U25")
}
