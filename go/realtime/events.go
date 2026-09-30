package realtime

import (
	"encoding/json"
	"strconv"
	"time"

	topstepx "github.com/CurtisTechSolutions/TopstepX/go"
	"github.com/CurtisTechSolutions/TopstepX/go/internal/jsonx"
	"github.com/shopspring/decimal"
)

// Event is delivered on a hub's Events channel. Use a type switch on the
// concrete types: *StateEvent, *AccountEvent, *OrderEvent, *PositionEvent,
// *UserTradeEvent, *QuoteEvent, *MarketTradesEvent, *DepthEvent,
// *SubscriptionErrorEvent and *UnknownEvent.
type Event interface {
	isEvent()
}

// ConnectionState is the kind of a StateEvent.
type ConnectionState int

// ConnectionState values.
const (
	// StateConnected follows a successful Connect.
	StateConnected ConnectionState = iota + 1
	// StateReconnecting precedes each reconnect attempt.
	StateReconnecting
	// StateReconnected follows a successful reconnect and the replay of the
	// subscription registry.
	StateReconnected
	// StateClosed is the last event before the channel closes.
	StateClosed
)

// String returns the state's name.
func (s ConnectionState) String() string {
	switch s {
	case StateConnected:
		return "connected"
	case StateReconnecting:
		return "reconnecting"
	case StateReconnected:
		return "reconnected"
	case StateClosed:
		return "closed"
	}
	return "ConnectionState(" + strconv.Itoa(int(s)) + ")"
}

// StateEvent reports a connection state change. A state event is always
// enqueued, even into a full buffer (the oldest buffered event is evicted);
// like any buffered event it can later be evicted under DropOldest when the
// consumer falls behind. The channel closing always follows StateClosed.
type StateEvent struct {
	// State is the new state.
	State ConnectionState
	// Attempt is the 1-based reconnect attempt for StateReconnecting and
	// StateReconnected.
	Attempt int
	// Err is the cause: for StateReconnecting, why the connection was lost
	// or why the previous attempt failed; for StateClosed, why the
	// connection ended, or nil after Close.
	Err error
}

// AccountEvent is a GatewayUserAccount update.
type AccountEvent struct {
	// Action is the envelope's action code when the payload was wrapped as
	// {"action": n, "data": {...}}. Its meaning is undocumented.
	Action *int32
	// Account is the decoded payload.
	Account GatewayUserAccount
	// Raw is the payload JSON, including undocumented members.
	Raw json.RawMessage
}

// OrderEvent is a GatewayUserOrder update.
type OrderEvent struct {
	// Action is the envelope's action code, if the payload was wrapped.
	Action *int32
	// Order is the decoded payload.
	Order GatewayUserOrder
	// Raw is the payload JSON, including undocumented members.
	Raw json.RawMessage
}

// PositionEvent is a GatewayUserPosition update.
type PositionEvent struct {
	// Action is the envelope's action code, if the payload was wrapped.
	Action *int32
	// Position is the decoded payload.
	Position GatewayUserPosition
	// Raw is the payload JSON, including undocumented members.
	Raw json.RawMessage
}

// UserTradeEvent is a GatewayUserTrade update.
type UserTradeEvent struct {
	// Action is the envelope's action code, if the payload was wrapped.
	Action *int32
	// Trade is the decoded payload.
	Trade GatewayUserTrade
	// Raw is the payload JSON, including undocumented members.
	Raw json.RawMessage
}

// QuoteEvent is a GatewayQuote update for a contract.
type QuoteEvent struct {
	// ContractID is the contract the quote belongs to.
	ContractID string
	// Quote is the decoded payload. Updates may carry only changed fields.
	Quote GatewayQuote
	// Raw is the payload JSON, including undocumented members.
	Raw json.RawMessage
}

// MarketTradesEvent carries the market trades of one GatewayTrade message.
type MarketTradesEvent struct {
	// ContractID is the contract the trades belong to.
	ContractID string
	// Trades are the trades, normalized to a list whether the server sent
	// one object or an array.
	Trades []GatewayTrade
	// Raw is the payload JSON.
	Raw json.RawMessage
}

// DepthEvent carries the market-depth entries of one GatewayDepth message.
type DepthEvent struct {
	// ContractID is the contract the entries belong to.
	ContractID string
	// Entries are the depth entries, normalized to a list whether the server
	// sent one object or an array.
	Entries []GatewayDepth
	// Raw is the payload JSON.
	Raw json.RawMessage
}

// SubscriptionErrorEvent reports a subscription that failed to be replayed
// after a reconnect. The subscription stays registered and is replayed again
// after the next reconnect.
type SubscriptionErrorEvent struct {
	// Method is the subscribe method, such as "SubscribeOrders".
	Method string
	// Arguments are the method arguments.
	Arguments []any
	// Err is the failure.
	Err error
}

// UnknownEvent is a server invocation with an unknown target, or one whose
// payload failed to decode. The connection is unaffected.
type UnknownEvent struct {
	// Target is the server event name.
	Target string
	// Arguments are the raw invocation arguments.
	Arguments []json.RawMessage
	// Err is the decode error, or nil for an unknown target.
	Err error
}

func (*StateEvent) isEvent()             {}
func (*AccountEvent) isEvent()           {}
func (*OrderEvent) isEvent()             {}
func (*PositionEvent) isEvent()          {}
func (*UserTradeEvent) isEvent()         {}
func (*QuoteEvent) isEvent()             {}
func (*MarketTradesEvent) isEvent()      {}
func (*DepthEvent) isEvent()             {}
func (*SubscriptionErrorEvent) isEvent() {}
func (*UnknownEvent) isEvent()           {}

// Payload models follow the official realtime guide; they are not assumed to
// match the REST models. Every field except identifiers is optional because
// updates may be partial.

// GatewayUserAccount is the payload of GatewayUserAccount events.
type GatewayUserAccount struct {
	// ID is the account identifier.
	ID int32 `json:"id"`
	// Name is the account name.
	Name *string `json:"name,omitempty"`
	// Balance is the account balance.
	Balance *decimal.Decimal `json:"balance,omitempty"`
	// CanTrade reports whether the account may trade.
	CanTrade *bool `json:"canTrade,omitempty"`
	// IsVisible reports whether the account is visible.
	IsVisible *bool `json:"isVisible,omitempty"`
	// Simulated reports whether the account is simulated.
	Simulated *bool `json:"simulated,omitempty"`
}

// MarshalJSON encodes the payload with decimals as JSON numbers.
func (m GatewayUserAccount) MarshalJSON() ([]byte, error) { return jsonx.Marshal(m) }

// UnmarshalJSON decodes the payload.
func (m *GatewayUserAccount) UnmarshalJSON(b []byte) error { return jsonx.Unmarshal(b, m) }

// GatewayUserPosition is the payload of GatewayUserPosition events.
type GatewayUserPosition struct {
	// ID is the position identifier.
	ID int32 `json:"id"`
	// AccountID is the owning account.
	AccountID int32 `json:"accountId"`
	// ContractID is the contract identifier.
	ContractID string `json:"contractId"`
	// CreationTimestamp is when the position was opened.
	CreationTimestamp *time.Time `json:"creationTimestamp,omitempty"`
	// Type is the position direction.
	Type *topstepx.PositionType `json:"type,omitempty"`
	// Size is the position quantity.
	Size *int32 `json:"size,omitempty"`
	// AveragePrice is the average entry price.
	AveragePrice *decimal.Decimal `json:"averagePrice,omitempty"`
}

// MarshalJSON encodes the payload with decimals as JSON numbers.
func (m GatewayUserPosition) MarshalJSON() ([]byte, error) { return jsonx.Marshal(m) }

// UnmarshalJSON decodes the payload, reading timestamps leniently.
func (m *GatewayUserPosition) UnmarshalJSON(b []byte) error { return jsonx.Unmarshal(b, m) }

// GatewayUserOrder is the payload of GatewayUserOrder events.
type GatewayUserOrder struct {
	// ID is the order identifier.
	ID int64 `json:"id"`
	// AccountID is the owning account.
	AccountID int32 `json:"accountId"`
	// ContractID is the contract identifier.
	ContractID string `json:"contractId"`
	// SymbolID is the symbol identifier.
	SymbolID string `json:"symbolId"`
	// CreationTimestamp is when the order was created.
	CreationTimestamp *time.Time `json:"creationTimestamp,omitempty"`
	// UpdateTimestamp is when the order last changed.
	UpdateTimestamp *time.Time `json:"updateTimestamp,omitempty"`
	// Status is the order status.
	Status *topstepx.OrderStatus `json:"status,omitempty"`
	// Type is the order type.
	Type *topstepx.OrderType `json:"type,omitempty"`
	// Side is the order side.
	Side *topstepx.OrderSide `json:"side,omitempty"`
	// Size is the order quantity.
	Size *int32 `json:"size,omitempty"`
	// LimitPrice is the limit price.
	LimitPrice *decimal.Decimal `json:"limitPrice,omitempty"`
	// StopPrice is the stop price.
	StopPrice *decimal.Decimal `json:"stopPrice,omitempty"`
	// FillVolume is the filled quantity, decoded leniently.
	FillVolume *int32 `json:"fillVolume,omitempty" jsonx:"lenient"`
	// FilledPrice is the fill price.
	FilledPrice *decimal.Decimal `json:"filledPrice,omitempty"`
	// CustomTag is the caller-supplied tag.
	CustomTag *string `json:"customTag,omitempty"`
}

// MarshalJSON encodes the payload with decimals as JSON numbers.
func (m GatewayUserOrder) MarshalJSON() ([]byte, error) { return jsonx.Marshal(m) }

// UnmarshalJSON decodes the payload, reading timestamps and volumes
// leniently.
func (m *GatewayUserOrder) UnmarshalJSON(b []byte) error { return jsonx.Unmarshal(b, m) }

// GatewayUserTrade is the payload of GatewayUserTrade events.
type GatewayUserTrade struct {
	// ID is the trade identifier.
	ID int64 `json:"id"`
	// AccountID is the owning account.
	AccountID int32 `json:"accountId"`
	// ContractID is the contract identifier.
	ContractID string `json:"contractId"`
	// CreationTimestamp is when the trade happened.
	CreationTimestamp *time.Time `json:"creationTimestamp,omitempty"`
	// Price is the execution price.
	Price *decimal.Decimal `json:"price,omitempty"`
	// ProfitAndLoss is the realized profit and loss; nil for a half-turn.
	ProfitAndLoss *decimal.Decimal `json:"profitAndLoss,omitempty"`
	// Fees are the trade fees.
	Fees *decimal.Decimal `json:"fees,omitempty"`
	// Side is the trade side.
	Side *topstepx.OrderSide `json:"side,omitempty"`
	// Size is the traded quantity.
	Size *int32 `json:"size,omitempty"`
	// Voided reports whether the trade was voided.
	Voided *bool `json:"voided,omitempty"`
	// OrderID is the order that produced the trade.
	OrderID int64 `json:"orderId"`
}

// MarshalJSON encodes the payload with decimals as JSON numbers.
func (m GatewayUserTrade) MarshalJSON() ([]byte, error) { return jsonx.Marshal(m) }

// UnmarshalJSON decodes the payload, reading timestamps leniently.
func (m *GatewayUserTrade) UnmarshalJSON(b []byte) error { return jsonx.Unmarshal(b, m) }

// GatewayQuote is the payload of GatewayQuote events. Updates may carry only
// the fields that changed.
type GatewayQuote struct {
	// Symbol is the symbol identifier.
	Symbol string `json:"symbol"`
	// SymbolName is the symbol's display name.
	SymbolName *string `json:"symbolName,omitempty"`
	// LastPrice is the last traded price.
	LastPrice *decimal.Decimal `json:"lastPrice,omitempty"`
	// BestBid is the best bid price.
	BestBid *decimal.Decimal `json:"bestBid,omitempty"`
	// BestAsk is the best ask price.
	BestAsk *decimal.Decimal `json:"bestAsk,omitempty"`
	// Change is the price change of the session.
	Change *decimal.Decimal `json:"change,omitempty"`
	// ChangePercent is the percentage price change of the session.
	ChangePercent *decimal.Decimal `json:"changePercent,omitempty"`
	// Open is the session's opening price.
	Open *decimal.Decimal `json:"open,omitempty"`
	// High is the session's highest price.
	High *decimal.Decimal `json:"high,omitempty"`
	// Low is the session's lowest price.
	Low *decimal.Decimal `json:"low,omitempty"`
	// Volume is the session volume, decoded leniently.
	Volume *int64 `json:"volume,omitempty" jsonx:"lenient"`
	// LastUpdated is when the quote last changed.
	LastUpdated *time.Time `json:"lastUpdated,omitempty"`
	// Timestamp is the quote timestamp.
	Timestamp *time.Time `json:"timestamp,omitempty"`
}

// MarshalJSON encodes the payload with decimals as JSON numbers.
func (m GatewayQuote) MarshalJSON() ([]byte, error) { return jsonx.Marshal(m) }

// UnmarshalJSON decodes the payload, reading timestamps and volumes
// leniently.
func (m *GatewayQuote) UnmarshalJSON(b []byte) error { return jsonx.Unmarshal(b, m) }

// GatewayDepth is one market-depth entry of GatewayDepth events.
type GatewayDepth struct {
	// Timestamp is the entry timestamp.
	Timestamp *time.Time `json:"timestamp,omitempty"`
	// Type is the depth entry type.
	Type *DomType `json:"type,omitempty"`
	// Price is the price level.
	Price *decimal.Decimal `json:"price,omitempty"`
	// Volume is the volume at the level, decoded leniently.
	Volume *int64 `json:"volume,omitempty" jsonx:"lenient"`
	// CurrentVolume is the current volume at the level, decoded leniently.
	CurrentVolume *int64 `json:"currentVolume,omitempty" jsonx:"lenient"`
}

// MarshalJSON encodes the entry with decimals as JSON numbers.
func (m GatewayDepth) MarshalJSON() ([]byte, error) { return jsonx.Marshal(m) }

// UnmarshalJSON decodes the entry, reading timestamps and volumes leniently.
func (m *GatewayDepth) UnmarshalJSON(b []byte) error { return jsonx.Unmarshal(b, m) }

// GatewayTrade is one market trade of GatewayTrade events.
type GatewayTrade struct {
	// SymbolID is the symbol identifier.
	SymbolID string `json:"symbolId"`
	// Price is the trade price.
	Price *decimal.Decimal `json:"price,omitempty"`
	// Timestamp is the trade timestamp.
	Timestamp *time.Time `json:"timestamp,omitempty"`
	// Type is the aggressor side.
	Type *TradeLogType `json:"type,omitempty"`
	// Volume is the traded volume, decoded leniently.
	Volume *int64 `json:"volume,omitempty" jsonx:"lenient"`
}

// MarshalJSON encodes the trade with decimals as JSON numbers.
func (m GatewayTrade) MarshalJSON() ([]byte, error) { return jsonx.Marshal(m) }

// UnmarshalJSON decodes the trade, reading timestamps and volumes leniently.
func (m *GatewayTrade) UnmarshalJSON(b []byte) error { return jsonx.Unmarshal(b, m) }

// DomType is the type of a market-depth entry. Unrecognized values are kept.
type DomType int32

// DomType values.
const (
	DomTypeUnknown    DomType = 0
	DomTypeAsk        DomType = 1
	DomTypeBid        DomType = 2
	DomTypeBestAsk    DomType = 3
	DomTypeBestBid    DomType = 4
	DomTypeTrade      DomType = 5
	DomTypeReset      DomType = 6
	DomTypeLow        DomType = 7
	DomTypeHigh       DomType = 8
	DomTypeNewBestBid DomType = 9
	DomTypeNewBestAsk DomType = 10
	DomTypeFill       DomType = 11
)

var domTypeNames = []string{"Unknown", "Ask", "Bid", "BestAsk", "BestBid", "Trade", "Reset", "Low", "High", "NewBestBid", "NewBestAsk", "Fill"}

// String returns the value's name, or "Unrecognized(<n>)".
func (v DomType) String() string { return enumString(domTypeNames, int32(v)) }

// TradeLogType is the aggressor side of a market trade. Unrecognized values
// are kept.
type TradeLogType int32

// TradeLogType values.
const (
	TradeLogTypeBuy  TradeLogType = 0
	TradeLogTypeSell TradeLogType = 1
)

var tradeLogTypeNames = []string{"Buy", "Sell"}

// String returns the value's name, or "Unrecognized(<n>)".
func (v TradeLogType) String() string { return enumString(tradeLogTypeNames, int32(v)) }

func enumString(names []string, v int32) string {
	if v >= 0 && int(v) < len(names) {
		return names[v]
	}
	return "Unrecognized(" + strconv.FormatInt(int64(v), 10) + ")"
}
