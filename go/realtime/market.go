package realtime

import (
	"context"
	"encoding/json"
	"fmt"

	topstepx "github.com/CurtisTechSolutions/TopstepX/go"
)

// Market hub server events.
const (
	EventGatewayQuote = "GatewayQuote"
	EventGatewayTrade = "GatewayTrade"
	EventGatewayDepth = "GatewayDepth"
)

// MarketHub is a connection to the SignalR market hub, which streams quotes,
// market trades and market depth per contract. It is safe for concurrent
// use.
//
// Create it with NewMarketHub or NewMarketHubForClient, read Events, call
// Connect, then subscribe. Subscriptions are replayed after every reconnect.
type MarketHub struct {
	e *engine
}

// NewMarketHub returns an unconnected market hub. tokens is called before
// every connection attempt; pass (*topstepx.Client).Token. The URL defaults
// to topstepx.DefaultMarketHubURL.
func NewMarketHub(tokens TokenProvider, opts ...Option) *MarketHub {
	cfg := newConfig(topstepx.DefaultMarketHubURL, opts)
	return &MarketHub{e: newEngine("market", cfg, tokens, decodeMarketEvent)}
}

// NewMarketHubForClient returns an unconnected market hub that takes its
// token, URL and logger from c and reports tokens rejected with HTTP 401 to
// c.InvalidateToken, so the next attempt logs in again. Later options
// override these settings.
func NewMarketHubForClient(c *topstepx.Client, opts ...Option) *MarketHub {
	base := []Option{WithURL(c.MarketHubURL()), WithLogger(c.Logger()), WithTokenRejected(c.InvalidateToken)}
	return NewMarketHub(c.Token, append(base, opts...)...)
}

func decodeMarketEvent(target string, args []json.RawMessage) []Event {
	switch target {
	case EventGatewayQuote, EventGatewayTrade, EventGatewayDepth:
	default:
		return []Event{unknownEvent(target, args, nil)}
	}
	contractID, data, err := marketArgs(args)
	if err != nil {
		return []Event{unknownEvent(target, args, err)}
	}
	switch target {
	case EventGatewayQuote:
		items, err := elements(data)
		if err != nil {
			return []Event{unknownEvent(target, args, err)}
		}
		out := make([]Event, 0, len(items))
		for i, item := range items {
			var q GatewayQuote
			if err := json.Unmarshal(item, &q); err != nil {
				out = append(out, unknownEvent(target, args, fmt.Errorf("element %d: %w", i, err)))
				continue
			}
			out = append(out, &QuoteEvent{ContractID: contractID, Quote: q, Raw: item})
		}
		return out
	case EventGatewayTrade:
		trades, err := decodeList[GatewayTrade](data)
		if err != nil {
			return []Event{unknownEvent(target, args, err)}
		}
		return []Event{&MarketTradesEvent{ContractID: contractID, Trades: trades, Raw: data}}
	default: // EventGatewayDepth
		entries, err := decodeList[GatewayDepth](data)
		if err != nil {
			return []Event{unknownEvent(target, args, err)}
		}
		return []Event{&DepthEvent{ContractID: contractID, Entries: entries, Raw: data}}
	}
}

// Connect opens the connection and performs the SignalR handshake. A failed
// first connect is not retried automatically; call Connect again.
func (h *MarketHub) Connect(ctx context.Context) error { return h.e.connect(ctx) }

// Events returns the event channel. It is buffered, never blocks the
// connection and is closed after the final StateClosed event.
func (h *MarketHub) Events() <-chan Event { return h.e.events }

// Dropped returns how many events were discarded because the consumer fell
// behind.
func (h *MarketHub) Dropped() uint64 { return h.e.dropped.Load() }

// Close stops reconnecting, sends a SignalR close message, closes the
// socket, fails pending invocations with ErrClosed and closes the event
// channel. It waits for the hub's goroutines to exit and is idempotent.
func (h *MarketHub) Close() error { return h.e.close() }

// Invoke calls any hub method and returns its raw result. It is not recorded
// for replay.
func (h *MarketHub) Invoke(ctx context.Context, method string, args ...any) (json.RawMessage, error) {
	return h.e.invoke(ctx, method, args)
}

// SubscribeContractQuotes subscribes to GatewayQuote events of the contract.
func (h *MarketHub) SubscribeContractQuotes(ctx context.Context, contractID string) error {
	return h.e.subscribe(ctx, "SubscribeContractQuotes", contractID)
}

// UnsubscribeContractQuotes cancels SubscribeContractQuotes for the contract.
func (h *MarketHub) UnsubscribeContractQuotes(ctx context.Context, contractID string) error {
	return h.e.unsubscribe(ctx, "UnsubscribeContractQuotes", "SubscribeContractQuotes", contractID)
}

// SubscribeContractTrades subscribes to GatewayTrade events of the contract.
func (h *MarketHub) SubscribeContractTrades(ctx context.Context, contractID string) error {
	return h.e.subscribe(ctx, "SubscribeContractTrades", contractID)
}

// UnsubscribeContractTrades cancels SubscribeContractTrades for the contract.
func (h *MarketHub) UnsubscribeContractTrades(ctx context.Context, contractID string) error {
	return h.e.unsubscribe(ctx, "UnsubscribeContractTrades", "SubscribeContractTrades", contractID)
}

// SubscribeContractMarketDepth subscribes to GatewayDepth events of the
// contract.
func (h *MarketHub) SubscribeContractMarketDepth(ctx context.Context, contractID string) error {
	return h.e.subscribe(ctx, "SubscribeContractMarketDepth", contractID)
}

// UnsubscribeContractMarketDepth cancels SubscribeContractMarketDepth for the
// contract.
func (h *MarketHub) UnsubscribeContractMarketDepth(ctx context.Context, contractID string) error {
	return h.e.unsubscribe(ctx, "UnsubscribeContractMarketDepth", "SubscribeContractMarketDepth", contractID)
}
