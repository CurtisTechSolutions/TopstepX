// Package realtime connects to the ProjectX Gateway SignalR hubs: the user
// hub (accounts, orders, positions, trades) and the market hub (quotes,
// market trades, market depth).
//
// It implements the ASP.NET Core SignalR JSON hub protocol v1 over WebSockets
// directly, as described in section 11 of spec/SDK_BEHAVIOR.md: it skips
// negotiation, sends the token as the access_token query parameter and as a
// bearer header, pings after 15 seconds of client silence, treats 30 seconds
// of server silence as a lost connection, reconnects with delays of 0, 1, 2,
// 5 and 10 seconds and then every 30 seconds (fetching a fresh token for
// every attempt), and replays successful subscriptions after each reconnect.
// When the server rejects a token with HTTP 401 during the upgrade, the hub
// reports it through WithTokenRejected (wired to
// (*topstepx.Client).InvalidateToken by the ForClient constructors), so the
// next attempt logs in again instead of reusing the rejected token.
//
// # Usage
//
//	client, _ := topstepx.NewClient(topstepx.WithCredentials(user, apiKey))
//	hub := realtime.NewUserHubForClient(client)
//	if err := hub.Connect(ctx); err != nil {
//		return err
//	}
//	defer hub.Close()
//	if err := hub.SubscribeOrders(ctx, accountID); err != nil {
//		return err
//	}
//	for ev := range hub.Events() {
//		switch ev := ev.(type) {
//		case *realtime.OrderEvent:
//			fmt.Println(ev.Order.ID, ev.Order.Status)
//		case *realtime.StateEvent:
//			fmt.Println("state:", ev.State, ev.Err)
//		}
//	}
//
// Events are delivered on one buffered channel together with connection
// state events. Delivery never blocks the connection: a consumer that falls
// behind loses events (see WithOverflowPolicy), which Dropped counts and the
// logger reports. Payloads are decoded leniently; one that cannot be decoded
// arrives as an *UnknownEvent and the connection is unaffected.
//
// Payload models follow the official realtime guide and are not assumed to
// match the REST models; every non-identifier field is optional because
// updates may be partial.
package realtime
