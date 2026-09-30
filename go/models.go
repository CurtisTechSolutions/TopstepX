package topstepx

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/CurtisTechSolutions/TopstepX/go/internal/jsonx"
	"github.com/shopspring/decimal"
)

// Ptr returns a pointer to a copy of v. It fills optional fields:
//
//	req.EndTimestamp = topstepx.Ptr(time.Now())
func Ptr[T any](v T) *T { return &v }

// TradingAccount is a trading account (spec schema TradingAccountModel).
type TradingAccount struct {
	// ID is the account identifier used by every account-scoped operation.
	ID int32 `json:"id"`
	// Name is the account name.
	Name string `json:"name"`
	// Balance is the account balance.
	Balance decimal.Decimal `json:"balance"`
	// CanTrade reports whether the account may trade.
	CanTrade bool `json:"canTrade"`
	// IsVisible reports whether the account is visible.
	IsVisible bool `json:"isVisible"`
	// Simulated reports whether the account is simulated. Some guide
	// examples omit it; it then decodes as false.
	Simulated bool `json:"simulated"`
}

// MarshalJSON encodes the account with its balance as a JSON number.
func (m TradingAccount) MarshalJSON() ([]byte, error) { return jsonx.Marshal(m) }

// UnmarshalJSON decodes the account.
func (m *TradingAccount) UnmarshalJSON(b []byte) error { return jsonx.Unmarshal(b, m) }

// Contract describes a tradable contract (spec schema ContractModel).
type Contract struct {
	// ID is the contract identifier, such as "CON.F.US.EP.U25".
	ID string `json:"id"`
	// Name is the contract name, such as "ESU5".
	Name string `json:"name"`
	// Description is a human-readable description.
	Description string `json:"description"`
	// TickSize is the minimum price increment.
	TickSize decimal.Decimal `json:"tickSize"`
	// TickValue is the monetary value of one tick.
	TickValue decimal.Decimal `json:"tickValue"`
	// ActiveContract reports whether this is the active contract.
	ActiveContract bool `json:"activeContract"`
	// SymbolID is the symbol identifier, such as "F.US.EP".
	SymbolID string `json:"symbolId"`
}

// MarshalJSON encodes the contract with decimals as JSON numbers.
func (m Contract) MarshalJSON() ([]byte, error) { return jsonx.Marshal(m) }

// UnmarshalJSON decodes the contract.
func (m *Contract) UnmarshalJSON(b []byte) error { return jsonx.Unmarshal(b, m) }

// AggregateBar is one historical OHLCV bar (spec schema AggregateBarModel).
// The spec's single-letter members map to descriptive field names.
type AggregateBar struct {
	// Time is the bar timestamp (JSON "t").
	Time time.Time `json:"t"`
	// Open is the opening price (JSON "o").
	Open decimal.Decimal `json:"o"`
	// High is the highest price (JSON "h").
	High decimal.Decimal `json:"h"`
	// Low is the lowest price (JSON "l").
	Low decimal.Decimal `json:"l"`
	// Close is the closing price (JSON "c").
	Close decimal.Decimal `json:"c"`
	// Volume is the traded volume (JSON "v").
	Volume int64 `json:"v"`
	// D is the undocumented "d" member (a date in Swagger), exposed raw. Its
	// semantics are not described by the guides.
	D *string `json:"d,omitempty"`
	// K is the undocumented "k" integer member, exposed raw. Its semantics are
	// not described by the guides.
	K *int64 `json:"k,omitempty"`
}

// MarshalJSON encodes the bar with decimals as JSON numbers.
func (m AggregateBar) MarshalJSON() ([]byte, error) { return jsonx.Marshal(m) }

// UnmarshalJSON decodes the bar, reading the timestamp leniently.
func (m *AggregateBar) UnmarshalJSON(b []byte) error { return jsonx.Unmarshal(b, m) }

// Order is an order (spec schema OrderModel).
type Order struct {
	// ID is the order identifier.
	ID int64 `json:"id"`
	// AccountID is the owning account.
	AccountID int32 `json:"accountId"`
	// ContractID is the contract identifier.
	ContractID string `json:"contractId"`
	// SymbolID is the symbol identifier.
	SymbolID string `json:"symbolId"`
	// CreationTimestamp is when the order was created.
	CreationTimestamp time.Time `json:"creationTimestamp"`
	// UpdateTimestamp is when the order last changed. The spec requires it,
	// but it is optional here so that a null update time does not fail
	// decoding.
	UpdateTimestamp *time.Time `json:"updateTimestamp"`
	// Status is the order status.
	Status OrderStatus `json:"status"`
	// Type is the order type.
	Type OrderType `json:"type"`
	// Side is the order side.
	Side OrderSide `json:"side"`
	// Size is the order quantity.
	Size int32 `json:"size"`
	// LimitPrice is the limit price, if any.
	LimitPrice *decimal.Decimal `json:"limitPrice,omitempty"`
	// StopPrice is the stop price, if any.
	StopPrice *decimal.Decimal `json:"stopPrice,omitempty"`
	// FillVolume is the filled quantity.
	FillVolume int32 `json:"fillVolume"`
	// FilledPrice is the fill price, if any.
	FilledPrice *decimal.Decimal `json:"filledPrice,omitempty"`
	// CustomTag is the caller-supplied tag, if any.
	CustomTag *string `json:"customTag,omitempty"`
	// TrailDistance is the trailing distance in ticks, if any.
	TrailDistance *int32 `json:"trailDistance,omitempty"`
	// TrailPrice is, for a returned trailing stop, the price DISTANCE (ticks
	// multiplied by tickSize), not the absolute level submitted in
	// PlaceOrderRequest.TrailPrice.
	TrailPrice *decimal.Decimal `json:"trailPrice,omitempty"`
	// ParentOrderID links a bracket order to its parent, if any.
	ParentOrderID *int64 `json:"parentOrderId,omitempty"`
	// LinkedOrderID links an order to its OCO sibling, if any.
	LinkedOrderID *int64 `json:"linkedOrderId,omitempty"`
}

// MarshalJSON encodes the order with decimals as JSON numbers.
func (m Order) MarshalJSON() ([]byte, error) { return jsonx.Marshal(m) }

// UnmarshalJSON decodes the order, reading timestamps leniently.
func (m *Order) UnmarshalJSON(b []byte) error { return jsonx.Unmarshal(b, m) }

// Position is an open position (spec schema PositionModel).
type Position struct {
	// ID is the position identifier.
	ID int32 `json:"id"`
	// AccountID is the owning account.
	AccountID int32 `json:"accountId"`
	// ContractID is the contract identifier.
	ContractID string `json:"contractId"`
	// ContractDisplayName is the contract's display name, if provided.
	ContractDisplayName *string `json:"contractDisplayName,omitempty"`
	// CreationTimestamp is when the position was opened.
	CreationTimestamp time.Time `json:"creationTimestamp"`
	// Type is the position direction.
	Type PositionType `json:"type"`
	// Size is the position quantity.
	Size int32 `json:"size"`
	// AveragePrice is the average entry price.
	AveragePrice decimal.Decimal `json:"averagePrice"`
}

// MarshalJSON encodes the position with decimals as JSON numbers.
func (m Position) MarshalJSON() ([]byte, error) { return jsonx.Marshal(m) }

// UnmarshalJSON decodes the position, reading timestamps leniently.
func (m *Position) UnmarshalJSON(b []byte) error { return jsonx.Unmarshal(b, m) }

// HalfTrade is an account trade (spec schema HalfTradeModel).
type HalfTrade struct {
	// ID is the trade identifier.
	ID int64 `json:"id"`
	// AccountID is the owning account.
	AccountID int32 `json:"accountId"`
	// ContractID is the contract identifier.
	ContractID string `json:"contractId"`
	// CreationTimestamp is when the trade happened.
	CreationTimestamp time.Time `json:"creationTimestamp"`
	// Price is the execution price.
	Price decimal.Decimal `json:"price"`
	// ProfitAndLoss is the realized profit and loss. Nil marks a half-turn
	// trade.
	ProfitAndLoss *decimal.Decimal `json:"profitAndLoss"`
	// Fees are the trade fees.
	Fees decimal.Decimal `json:"fees"`
	// Commissions are the trade commissions, if provided.
	Commissions *decimal.Decimal `json:"commissions,omitempty"`
	// Side is the trade side.
	Side OrderSide `json:"side"`
	// Size is the traded quantity.
	Size int32 `json:"size"`
	// Voided reports whether the trade was voided.
	Voided bool `json:"voided"`
	// OrderID is the order that produced the trade.
	OrderID int64 `json:"orderId"`
}

// MarshalJSON encodes the trade with decimals as JSON numbers.
func (m HalfTrade) MarshalJSON() ([]byte, error) { return jsonx.Marshal(m) }

// UnmarshalJSON decodes the trade, reading timestamps leniently.
func (m *HalfTrade) UnmarshalJSON(b []byte) error { return jsonx.Unmarshal(b, m) }

// ProblemDetails is an RFC 7807 error body (spec schema ProblemDetails).
// Members other than the five standard ones are kept in Extensions.
type ProblemDetails struct {
	// Type is a URI reference identifying the problem type.
	Type *string `json:"type,omitempty"`
	// Title is a short summary of the problem type.
	Title *string `json:"title,omitempty"`
	// Status is the HTTP status code.
	Status *int32 `json:"status,omitempty"`
	// Detail explains this occurrence of the problem.
	Detail *string `json:"detail,omitempty"`
	// Instance identifies this occurrence of the problem.
	Instance *string `json:"instance,omitempty"`
	// Extensions holds every other member, such as "traceId" or "errors",
	// as raw JSON.
	Extensions map[string]json.RawMessage `json:"-"`
}

var problemMembers = map[string]bool{"type": true, "title": true, "status": true, "detail": true, "instance": true}

type problemFields ProblemDetails

// UnmarshalJSON decodes the standard members and collects the rest in
// Extensions.
func (p *ProblemDetails) UnmarshalJSON(b []byte) error {
	var known problemFields
	if err := json.Unmarshal(b, &known); err != nil {
		return err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(b, &all); err != nil {
		return err
	}
	*p = ProblemDetails(known)
	for k, v := range all {
		if problemMembers[k] {
			continue
		}
		if p.Extensions == nil {
			p.Extensions = make(map[string]json.RawMessage)
		}
		p.Extensions[k] = v
	}
	return nil
}

// MarshalJSON encodes the standard members followed by the extensions in key
// order.
func (p ProblemDetails) MarshalJSON() ([]byte, error) {
	known, err := json.Marshal(problemFields(p))
	if err != nil {
		return nil, err
	}
	if len(p.Extensions) == 0 {
		return known, nil
	}
	keys := make([]string, 0, len(p.Extensions))
	for k := range p.Extensions {
		if !problemMembers[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	buf.Write(known[:len(known)-1])
	first := len(known) == 2
	for _, k := range keys {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		name, _ := json.Marshal(k)
		buf.Write(name)
		buf.WriteByte(':')
		v := p.Extensions[k]
		if len(bytes.TrimSpace(v)) == 0 {
			v = json.RawMessage("null")
		}
		buf.Write(v)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// summary returns title and detail for error messages.
func (p *ProblemDetails) summary() string {
	var parts []string
	if p.Title != nil && *p.Title != "" {
		parts = append(parts, *p.Title)
	}
	if p.Detail != nil && *p.Detail != "" {
		parts = append(parts, *p.Detail)
	}
	return strings.Join(parts, ": ")
}

// parseProblem returns the body as ProblemDetails when it is a JSON object
// with at least one standard member.
func parseProblem(body []byte) *ProblemDetails {
	body = bytes.TrimSpace(body)
	if len(body) == 0 || body[0] != '{' {
		return nil
	}
	var p ProblemDetails
	if err := json.Unmarshal(body, &p); err != nil {
		return nil
	}
	if p.Type == nil && p.Title == nil && p.Status == nil && p.Detail == nil && p.Instance == nil {
		return nil
	}
	return &p
}
