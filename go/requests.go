package topstepx

import (
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/CurtisTechSolutions/TopstepX/go/internal/jsonx"
	"github.com/shopspring/decimal"
)

const redacted = "[REDACTED]"

// SearchAccountRequest is the Account_SearchAccounts request.
type SearchAccountRequest struct {
	// OnlyActiveAccounts limits the result to active accounts.
	OnlyActiveAccounts bool `json:"onlyActiveAccounts"`
}

// LoginAPIKeyRequest is the Auth_LoginKey request (spec schema
// LoginApiKeyRequest). Printing it with fmt or slog redacts the API key.
type LoginAPIKeyRequest struct {
	// UserName is the platform sign-in username (case-insensitive; not an
	// email address or account label).
	UserName string `json:"userName"`
	// APIKey is the API key issued in the firm's trading platform. It is a
	// secret.
	APIKey string `json:"apiKey"`
}

// String returns a representation with the API key redacted.
func (r LoginAPIKeyRequest) String() string {
	return fmt.Sprintf("LoginAPIKeyRequest{UserName: %q, APIKey: %s}", r.UserName, redacted)
}

// GoString returns a Go-syntax representation with the API key redacted.
func (r LoginAPIKeyRequest) GoString() string {
	return fmt.Sprintf("topstepx.LoginAPIKeyRequest{UserName:%q, APIKey:%q}", r.UserName, redacted)
}

// Format implements fmt.Formatter so that no verb prints the API key.
func (r LoginAPIKeyRequest) Format(f fmt.State, verb rune) { formatRedacted(f, verb, r) }

// LogValue implements slog.LogValuer with the API key redacted.
func (r LoginAPIKeyRequest) LogValue() slog.Value {
	return slog.GroupValue(slog.String("userName", r.UserName), slog.String("apiKey", redacted))
}

// LoginAppRequest is the Auth_LoginApp request. The current authentication
// guide does not support application login; use LoginAPIKeyRequest.
// Printing it with fmt or slog redacts the password and verify key.
type LoginAppRequest struct {
	// UserName is the platform username.
	UserName string `json:"userName"`
	// Password is the account password. It is a secret.
	Password string `json:"password"`
	// DeviceID identifies the device.
	DeviceID string `json:"deviceId"`
	// AppID identifies the application.
	AppID string `json:"appId"`
	// VerifyKey is the application verification key. It is a secret.
	VerifyKey string `json:"verifyKey"`
}

// String returns a representation with secrets redacted.
func (r LoginAppRequest) String() string {
	return fmt.Sprintf("LoginAppRequest{UserName: %q, Password: %s, DeviceID: %q, AppID: %q, VerifyKey: %s}",
		r.UserName, redacted, r.DeviceID, r.AppID, redacted)
}

// GoString returns a Go-syntax representation with secrets redacted.
func (r LoginAppRequest) GoString() string {
	return fmt.Sprintf("topstepx.LoginAppRequest{UserName:%q, Password:%q, DeviceID:%q, AppID:%q, VerifyKey:%q}",
		r.UserName, redacted, r.DeviceID, r.AppID, redacted)
}

// Format implements fmt.Formatter so that no verb prints a secret.
func (r LoginAppRequest) Format(f fmt.State, verb rune) { formatRedacted(f, verb, r) }

// LogValue implements slog.LogValuer with secrets redacted.
func (r LoginAppRequest) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("userName", r.UserName), slog.String("password", redacted),
		slog.String("deviceId", r.DeviceID), slog.String("appId", r.AppID),
		slog.String("verifyKey", redacted))
}

// redactedValue is implemented by types that must never print secrets.
type redactedValue interface {
	String() string
	GoString() string
}

// formatRedacted prints the redacted String form for every verb, or the
// GoString form for %#v.
func formatRedacted(f fmt.State, verb rune, v redactedValue) {
	if verb == 'v' && f.Flag('#') {
		_, _ = io.WriteString(f, v.GoString())
		return
	}
	_, _ = io.WriteString(f, v.String())
}

// SearchContractRequest is the Contract_SearchContracts request.
type SearchContractRequest struct {
	// SearchText is matched against contract names; at most 20 contracts are
	// returned.
	SearchText string `json:"searchText"`
	// Live selects the live (true) or simulated (false) data subscription.
	// It does not describe the account type.
	Live bool `json:"live"`
}

// SearchContractByIDRequest is the Contract_SearchContractById request
// (spec schema SearchContractByIdRequest).
type SearchContractByIDRequest struct {
	// ContractID is the contract identifier.
	ContractID string `json:"contractId"`
}

// ListAvailableContractRequest is the Contract_AvailableContracts request.
type ListAvailableContractRequest struct {
	// Live selects the live (true) or simulated (false) data subscription.
	Live bool `json:"live"`
}

// MaxBars is the largest RetrieveBarRequest.Limit the gateway accepts.
const MaxBars = 20000

// RetrieveBarRequest is the History_GetBars request. History_GetBars has its
// own rate limit of 50 requests per 30 seconds.
type RetrieveBarRequest struct {
	// ContractID is the string contract identifier.
	ContractID string `json:"contractId"`
	// Live selects the live (true) or simulated (false) data subscription.
	Live bool `json:"live"`
	// StartTime is the start of the range.
	StartTime time.Time `json:"startTime"`
	// EndTime is the end of the range.
	EndTime time.Time `json:"endTime"`
	// Unit is the aggregation unit. The guide documents Second through Month.
	Unit AggregateBarUnit `json:"unit"`
	// UnitNumber is the number of units per bar.
	UnitNumber int32 `json:"unitNumber"`
	// Limit is the maximum number of bars, at most MaxBars.
	Limit int32 `json:"limit"`
	// IncludePartialBar includes the current, incomplete bar.
	IncludePartialBar bool `json:"includePartialBar"`
}

// MarshalJSON encodes the request with RFC 3339 UTC timestamps.
func (r RetrieveBarRequest) MarshalJSON() ([]byte, error) { return jsonx.Marshal(r) }

// UnmarshalJSON decodes the request, reading timestamps leniently.
func (r *RetrieveBarRequest) UnmarshalJSON(b []byte) error { return jsonx.Unmarshal(b, r) }

// SearchOrderRequest is the Order_SearchOrders request.
type SearchOrderRequest struct {
	// AccountID is the account to search.
	AccountID int32 `json:"accountId"`
	// StartTimestamp is the start of the creation-time range.
	StartTimestamp time.Time `json:"startTimestamp"`
	// EndTimestamp is the optional end of the creation-time range.
	EndTimestamp *time.Time `json:"endTimestamp,omitempty"`
}

// MarshalJSON encodes the request with RFC 3339 UTC timestamps.
func (r SearchOrderRequest) MarshalJSON() ([]byte, error) { return jsonx.Marshal(r) }

// UnmarshalJSON decodes the request, reading timestamps leniently.
func (r *SearchOrderRequest) UnmarshalJSON(b []byte) error { return jsonx.Unmarshal(b, r) }

// SearchOrderByIDRequest is the Order_SearchOrderById request (spec schema
// SearchOrderByIdRequest).
type SearchOrderByIDRequest struct {
	// AccountID is the owning account; it must belong to the caller.
	AccountID int32 `json:"accountId"`
	// OrderID is the order identifier.
	OrderID int64 `json:"orderId"`
}

// SearchOpenOrderRequest is the Order_SearchOpenOrders request.
type SearchOpenOrderRequest struct {
	// AccountID is the account to search.
	AccountID int32 `json:"accountId"`
}

// SearchOrdersQueryRequest is the Order_SearchOrdersQuery request. The
// gateway's pagination defaults and bounds are not published.
type SearchOrdersQueryRequest struct {
	// Filter selects the orders.
	Filter GatewayOrderFilter `json:"filter"`
	// PageSize is the optional page size.
	PageSize *int32 `json:"pageSize,omitempty"`
	// PageOffset is the optional page offset.
	PageOffset *int32 `json:"pageOffset,omitempty"`
	// SortBy is the optional sort key.
	SortBy *OrderSortField `json:"sortBy,omitempty"`
	// SortDirection is the optional sort order.
	SortDirection *SortDirection `json:"sortDirection,omitempty"`
	// IncludeTotalCount asks for totalCount in the response.
	IncludeTotalCount *bool `json:"includeTotalCount,omitempty"`
}

// GatewayOrderFilter is the filter of SearchOrdersQueryRequest.
type GatewayOrderFilter struct {
	// AccountID is the account to search.
	AccountID int32 `json:"accountId"`
	// Statuses optionally restricts the order statuses.
	Statuses []OrderStatus `json:"statuses,omitempty"`
	// ContractID optionally restricts the contract.
	ContractID *string `json:"contractId,omitempty"`
	// CreatedAfter optionally bounds the creation time from below.
	CreatedAfter *time.Time `json:"createdAfter,omitempty"`
	// CreatedBefore optionally bounds the creation time from above.
	CreatedBefore *time.Time `json:"createdBefore,omitempty"`
}

// MarshalJSON encodes the filter with RFC 3339 UTC timestamps.
func (r GatewayOrderFilter) MarshalJSON() ([]byte, error) { return jsonx.Marshal(r) }

// UnmarshalJSON decodes the filter, reading timestamps leniently.
func (r *GatewayOrderFilter) UnmarshalJSON(b []byte) error { return jsonx.Unmarshal(b, r) }

// PlaceOrderRequest is the Order_PlaceOrder request. Build one with
// NewMarketOrder, NewLimitOrder, NewStopOrder, NewTrailingStopOrder,
// NewJoinBidOrder or NewJoinAskOrder and refine it with the With methods.
//
// Prices are sent as JSON numbers. Orders are real: use simulated accounts
// while developing.
type PlaceOrderRequest struct {
	// AccountID is the account that places the order.
	AccountID int32 `json:"accountId"`
	// ContractID is the contract to trade.
	ContractID string `json:"contractId"`
	// Type must be Limit, Market, Stop, TrailingStop, JoinBid or JoinAsk.
	Type OrderType `json:"type"`
	// Side is Bid (buy) or Ask (sell).
	Side OrderSide `json:"side"`
	// Size is the order quantity.
	Size int32 `json:"size"`
	// LimitPrice is the limit price for limit orders.
	LimitPrice *decimal.Decimal `json:"limitPrice,omitempty"`
	// StopPrice is the stop price for stop orders.
	StopPrice *decimal.Decimal `json:"stopPrice,omitempty"`
	// TrailPrice is required for TrailingStop orders. It is an ABSOLUTE stop
	// price aligned to the contract's tickSize, not a distance; the server
	// converts its gap from the last price into whole ticks and rejects
	// initial trails above 1,000 ticks. (Order.TrailPrice in responses is a
	// distance.)
	TrailPrice *decimal.Decimal `json:"trailPrice,omitempty"`
	// CustomTag is an optional tag that must be unique within the account.
	CustomTag *string `json:"customTag,omitempty"`
	// StopLossBracket attaches a stop-loss bracket. Brackets require the
	// account's Auto OCO Brackets mode.
	StopLossBracket *PlaceOrderBracket `json:"stopLossBracket,omitempty"`
	// TakeProfitBracket attaches a take-profit bracket. Brackets require the
	// account's Auto OCO Brackets mode.
	TakeProfitBracket *PlaceOrderBracket `json:"takeProfitBracket,omitempty"`
}

// MarshalJSON encodes the request with prices as JSON numbers.
func (r PlaceOrderRequest) MarshalJSON() ([]byte, error) { return jsonx.Marshal(r) }

// UnmarshalJSON decodes the request.
func (r *PlaceOrderRequest) UnmarshalJSON(b []byte) error { return jsonx.Unmarshal(b, r) }

// PlaceOrderBracket is a stop-loss or take-profit bracket, placed the given
// number of ticks from the fill.
type PlaceOrderBracket struct {
	// Ticks is the bracket distance in ticks.
	Ticks int32 `json:"ticks"`
	// Type must be Limit, Market, Stop, TrailingStop, JoinBid or JoinAsk.
	Type OrderType `json:"type"`
}

// NewMarketOrder returns a market order request.
func NewMarketOrder(accountID int32, contractID string, side OrderSide, size int32) PlaceOrderRequest {
	return PlaceOrderRequest{AccountID: accountID, ContractID: contractID, Type: OrderTypeMarket, Side: side, Size: size}
}

// NewLimitOrder returns a limit order request at limitPrice.
func NewLimitOrder(accountID int32, contractID string, side OrderSide, size int32, limitPrice decimal.Decimal) PlaceOrderRequest {
	r := PlaceOrderRequest{AccountID: accountID, ContractID: contractID, Type: OrderTypeLimit, Side: side, Size: size}
	r.LimitPrice = &limitPrice
	return r
}

// NewStopOrder returns a stop order request triggered at stopPrice.
func NewStopOrder(accountID int32, contractID string, side OrderSide, size int32, stopPrice decimal.Decimal) PlaceOrderRequest {
	r := PlaceOrderRequest{AccountID: accountID, ContractID: contractID, Type: OrderTypeStop, Side: side, Size: size}
	r.StopPrice = &stopPrice
	return r
}

// NewTrailingStopOrder returns a trailing-stop order request. trailPrice is
// the ABSOLUTE initial stop price aligned to tickSize, not a distance.
func NewTrailingStopOrder(accountID int32, contractID string, side OrderSide, size int32, trailPrice decimal.Decimal) PlaceOrderRequest {
	r := PlaceOrderRequest{AccountID: accountID, ContractID: contractID, Type: OrderTypeTrailingStop, Side: side, Size: size}
	r.TrailPrice = &trailPrice
	return r
}

// NewJoinBidOrder returns a JoinBid order request.
func NewJoinBidOrder(accountID int32, contractID string, side OrderSide, size int32) PlaceOrderRequest {
	return PlaceOrderRequest{AccountID: accountID, ContractID: contractID, Type: OrderTypeJoinBid, Side: side, Size: size}
}

// NewJoinAskOrder returns a JoinAsk order request.
func NewJoinAskOrder(accountID int32, contractID string, side OrderSide, size int32) PlaceOrderRequest {
	return PlaceOrderRequest{AccountID: accountID, ContractID: contractID, Type: OrderTypeJoinAsk, Side: side, Size: size}
}

// WithCustomTag returns a copy of r with the custom tag set. The tag must be
// unique within the account.
func (r PlaceOrderRequest) WithCustomTag(tag string) PlaceOrderRequest {
	r.CustomTag = &tag
	return r
}

// WithStopLossBracket returns a copy of r with a stop-loss bracket ticks away
// from the fill, placed as an order of type typ.
func (r PlaceOrderRequest) WithStopLossBracket(ticks int32, typ OrderType) PlaceOrderRequest {
	r.StopLossBracket = &PlaceOrderBracket{Ticks: ticks, Type: typ}
	return r
}

// WithTakeProfitBracket returns a copy of r with a take-profit bracket ticks
// away from the fill, placed as an order of type typ.
func (r PlaceOrderRequest) WithTakeProfitBracket(ticks int32, typ OrderType) PlaceOrderRequest {
	r.TakeProfitBracket = &PlaceOrderBracket{Ticks: ticks, Type: typ}
	return r
}

// CancelOrderRequest is the Order_CancelOrder request.
type CancelOrderRequest struct {
	// AccountID is the owning account.
	AccountID int32 `json:"accountId"`
	// OrderID is the order to cancel.
	OrderID int64 `json:"orderId"`
}

// ModifyOrderRequest is the Order_ModifyOrder request. Nil fields are left
// unchanged and are omitted from the request. Prices are sent as JSON numbers.
type ModifyOrderRequest struct {
	// AccountID is the owning account.
	AccountID int32 `json:"accountId"`
	// OrderID is the order to modify.
	OrderID int64 `json:"orderId"`
	// Size is the new quantity.
	Size *int32 `json:"size,omitempty"`
	// LimitPrice is the new limit price.
	LimitPrice *decimal.Decimal `json:"limitPrice,omitempty"`
	// StopPrice is the new stop price.
	StopPrice *decimal.Decimal `json:"stopPrice,omitempty"`
	// TrailPrice is the new ABSOLUTE trailing-stop level, not a distance. The
	// server recalculates the tick distance from its current last price.
	TrailPrice *decimal.Decimal `json:"trailPrice,omitempty"`
}

// MarshalJSON encodes the request with prices as JSON numbers.
func (r ModifyOrderRequest) MarshalJSON() ([]byte, error) { return jsonx.Marshal(r) }

// UnmarshalJSON decodes the request.
func (r *ModifyOrderRequest) UnmarshalJSON(b []byte) error { return jsonx.Unmarshal(b, r) }

// SearchPositionRequest is the Position_SearchOpenPositions request.
type SearchPositionRequest struct {
	// AccountID is the account to search.
	AccountID int32 `json:"accountId"`
}

// CloseContractPositionRequest is the Position_CloseContractPosition request.
type CloseContractPositionRequest struct {
	// AccountID is the owning account.
	AccountID int32 `json:"accountId"`
	// ContractID is the contract whose position is closed.
	ContractID string `json:"contractId"`
}

// PartialCloseContractPositionRequest is the
// Position_PartialCloseContractPosition request.
type PartialCloseContractPositionRequest struct {
	// AccountID is the owning account.
	AccountID int32 `json:"accountId"`
	// ContractID is the contract whose position is reduced.
	ContractID string `json:"contractId"`
	// Size is the quantity to close: at least 1 and no larger than the open
	// position.
	Size int32 `json:"size"`
}

// SearchTradeRequest is the Trade_SearchHalfTurnTrades request.
// StartTimestamp is required, following the guide (Swagger requires only
// accountId).
type SearchTradeRequest struct {
	// AccountID is the account to search.
	AccountID int32 `json:"accountId"`
	// StartTimestamp is the start of the range.
	StartTimestamp time.Time `json:"startTimestamp"`
	// EndTimestamp is the optional end of the range.
	EndTimestamp *time.Time `json:"endTimestamp,omitempty"`
}

// MarshalJSON encodes the request with RFC 3339 UTC timestamps.
func (r SearchTradeRequest) MarshalJSON() ([]byte, error) { return jsonx.Marshal(r) }

// UnmarshalJSON decodes the request, reading timestamps leniently.
func (r *SearchTradeRequest) UnmarshalJSON(b []byte) error { return jsonx.Unmarshal(b, r) }
