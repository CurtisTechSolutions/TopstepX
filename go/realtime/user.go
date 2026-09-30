package realtime

import (
	"context"
	"encoding/json"

	topstepx "github.com/CurtisTechSolutions/TopstepX/go"
)

// User hub server events.
const (
	EventGatewayUserAccount  = "GatewayUserAccount"
	EventGatewayUserOrder    = "GatewayUserOrder"
	EventGatewayUserPosition = "GatewayUserPosition"
	EventGatewayUserTrade    = "GatewayUserTrade"
)

// UserHub is a connection to the SignalR user hub, which streams account,
// order, position and trade updates. It is safe for concurrent use.
//
// Create it with NewUserHub or NewUserHubForClient, read Events, call
// Connect, then subscribe. Subscriptions are replayed after every reconnect.
type UserHub struct {
	e *engine
}

// NewUserHub returns an unconnected user hub. tokens is called before every
// connection attempt; pass (*topstepx.Client).Token. The URL defaults to
// topstepx.DefaultUserHubURL.
func NewUserHub(tokens TokenProvider, opts ...Option) *UserHub {
	cfg := newConfig(topstepx.DefaultUserHubURL, opts)
	return &UserHub{e: newEngine("user", cfg, tokens, decodeUserEvent)}
}

// NewUserHubForClient returns an unconnected user hub that takes its token,
// URL and logger from c and reports tokens rejected with HTTP 401 to
// c.InvalidateToken, so the next attempt logs in again. Later options
// override these settings.
func NewUserHubForClient(c *topstepx.Client, opts ...Option) *UserHub {
	base := []Option{WithURL(c.UserHubURL()), WithLogger(c.Logger()), WithTokenRejected(c.InvalidateToken)}
	return NewUserHub(c.Token, append(base, opts...)...)
}

func decodeUserEvent(target string, args []json.RawMessage) []Event {
	switch target {
	case EventGatewayUserAccount:
		return decodeUserPayload(target, args, func(action *int32, v GatewayUserAccount, raw json.RawMessage) Event {
			return &AccountEvent{Action: action, Account: v, Raw: raw}
		})
	case EventGatewayUserOrder:
		return decodeUserPayload(target, args, func(action *int32, v GatewayUserOrder, raw json.RawMessage) Event {
			return &OrderEvent{Action: action, Order: v, Raw: raw}
		})
	case EventGatewayUserPosition:
		return decodeUserPayload(target, args, func(action *int32, v GatewayUserPosition, raw json.RawMessage) Event {
			return &PositionEvent{Action: action, Position: v, Raw: raw}
		})
	case EventGatewayUserTrade:
		return decodeUserPayload(target, args, func(action *int32, v GatewayUserTrade, raw json.RawMessage) Event {
			return &UserTradeEvent{Action: action, Trade: v, Raw: raw}
		})
	}
	return []Event{unknownEvent(target, args, nil)}
}

// Connect opens the connection and performs the SignalR handshake. A failed
// first connect is not retried automatically; call Connect again.
func (h *UserHub) Connect(ctx context.Context) error { return h.e.connect(ctx) }

// Events returns the event channel. It is buffered, never blocks the
// connection and is closed after the final StateClosed event.
func (h *UserHub) Events() <-chan Event { return h.e.events }

// Dropped returns how many events were discarded because the consumer fell
// behind.
func (h *UserHub) Dropped() uint64 { return h.e.dropped.Load() }

// Close stops reconnecting, sends a SignalR close message, closes the
// socket, fails pending invocations with ErrClosed and closes the event
// channel. It waits for the hub's goroutines to exit and is idempotent.
func (h *UserHub) Close() error { return h.e.close() }

// Invoke calls any hub method and returns its raw result. It is not recorded
// for replay.
func (h *UserHub) Invoke(ctx context.Context, method string, args ...any) (json.RawMessage, error) {
	return h.e.invoke(ctx, method, args)
}

// SubscribeAccounts subscribes to GatewayUserAccount events.
func (h *UserHub) SubscribeAccounts(ctx context.Context) error {
	return h.e.subscribe(ctx, "SubscribeAccounts")
}

// UnsubscribeAccounts cancels SubscribeAccounts.
func (h *UserHub) UnsubscribeAccounts(ctx context.Context) error {
	return h.e.unsubscribe(ctx, "UnsubscribeAccounts", "SubscribeAccounts")
}

// SubscribeOrders subscribes to GatewayUserOrder events of the account.
func (h *UserHub) SubscribeOrders(ctx context.Context, accountID int32) error {
	return h.e.subscribe(ctx, "SubscribeOrders", accountID)
}

// UnsubscribeOrders cancels SubscribeOrders for the account.
func (h *UserHub) UnsubscribeOrders(ctx context.Context, accountID int32) error {
	return h.e.unsubscribe(ctx, "UnsubscribeOrders", "SubscribeOrders", accountID)
}

// SubscribePositions subscribes to GatewayUserPosition events of the account.
func (h *UserHub) SubscribePositions(ctx context.Context, accountID int32) error {
	return h.e.subscribe(ctx, "SubscribePositions", accountID)
}

// UnsubscribePositions cancels SubscribePositions for the account.
func (h *UserHub) UnsubscribePositions(ctx context.Context, accountID int32) error {
	return h.e.unsubscribe(ctx, "UnsubscribePositions", "SubscribePositions", accountID)
}

// SubscribeTrades subscribes to GatewayUserTrade events of the account.
func (h *UserHub) SubscribeTrades(ctx context.Context, accountID int32) error {
	return h.e.subscribe(ctx, "SubscribeTrades", accountID)
}

// UnsubscribeTrades cancels SubscribeTrades for the account.
func (h *UserHub) UnsubscribeTrades(ctx context.Context, accountID int32) error {
	return h.e.unsubscribe(ctx, "UnsubscribeTrades", "SubscribeTrades", accountID)
}
