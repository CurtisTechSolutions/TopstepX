package topstepx

import (
	"fmt"
	"log/slog"
)

// envelope is the common part of every response except Status_Ping's.
type envelope struct {
	success bool
	code    int32
	name    string
	message *string
}

// enveloped is implemented by every response type.
type enveloped interface {
	envelope() envelope
}

func isFailure(success bool, code int32) bool { return !success || code != 0 }

// SearchAccountResponse is the Account_SearchAccounts response.
// Missing arrays decode as nil slices.
type SearchAccountResponse struct {
	// Success is false for a business failure.
	Success bool `json:"success"`
	// ErrorCode is zero on success.
	ErrorCode SearchAccountErrorCode `json:"errorCode"`
	// ErrorMessage describes a failure; the gateway may leave it null.
	ErrorMessage *string `json:"errorMessage,omitempty"`
	// HasBusinessFailure is the gateway's own failure flag; it decodes as
	// false when absent. Failures are detected from Success and ErrorCode.
	HasBusinessFailure bool `json:"hasBusinessFailure"`
	// Accounts are the matching accounts.
	Accounts []TradingAccount `json:"accounts,omitempty"`
}

// IsBusinessFailure reports whether success is false or errorCode is non-zero.
func (r *SearchAccountResponse) IsBusinessFailure() bool {
	return isFailure(r.Success, int32(r.ErrorCode))
}

func (r *SearchAccountResponse) envelope() envelope {
	return envelope{r.Success, int32(r.ErrorCode), r.ErrorCode.String(), r.ErrorMessage}
}

// LoginResponse is the Auth_LoginKey and Auth_LoginApp response.
type LoginResponse struct {
	// Success is false for a business failure.
	Success bool `json:"success"`
	// ErrorCode is zero on success.
	ErrorCode LoginErrorCode `json:"errorCode"`
	// ErrorMessage describes a failure; the gateway may leave it null.
	ErrorMessage *string `json:"errorMessage,omitempty"`
	// HasBusinessFailure is the gateway's own failure flag; it decodes as
	// false when absent. Failures are detected from Success and ErrorCode.
	HasBusinessFailure bool `json:"hasBusinessFailure"`
	// Token is the session JWT, null on failure. It is a secret.
	Token *string `json:"token,omitempty"`
}

// IsBusinessFailure reports whether success is false or errorCode is non-zero.
func (r *LoginResponse) IsBusinessFailure() bool { return isFailure(r.Success, int32(r.ErrorCode)) }

func (r *LoginResponse) envelope() envelope {
	return envelope{r.Success, int32(r.ErrorCode), r.ErrorCode.String(), r.ErrorMessage}
}

// LogoutResponse is the Auth_Logout response.
type LogoutResponse struct {
	// Success is false for a business failure.
	Success bool `json:"success"`
	// ErrorCode is zero on success.
	ErrorCode LogoutErrorCode `json:"errorCode"`
	// ErrorMessage describes a failure; the gateway may leave it null.
	ErrorMessage *string `json:"errorMessage,omitempty"`
	// HasBusinessFailure is the gateway's own failure flag; it decodes as
	// false when absent. Failures are detected from Success and ErrorCode.
	HasBusinessFailure bool `json:"hasBusinessFailure"`
}

// IsBusinessFailure reports whether success is false or errorCode is non-zero.
func (r *LogoutResponse) IsBusinessFailure() bool { return isFailure(r.Success, int32(r.ErrorCode)) }

func (r *LogoutResponse) envelope() envelope {
	return envelope{r.Success, int32(r.ErrorCode), r.ErrorCode.String(), r.ErrorMessage}
}

// ValidateResponse is the Auth_Validate response.
type ValidateResponse struct {
	// Success is false for a business failure.
	Success bool `json:"success"`
	// ErrorCode is zero on success.
	ErrorCode ValidateErrorCode `json:"errorCode"`
	// ErrorMessage describes a failure; the gateway may leave it null.
	ErrorMessage *string `json:"errorMessage,omitempty"`
	// HasBusinessFailure is the gateway's own failure flag; it decodes as
	// false when absent. Failures are detected from Success and ErrorCode.
	HasBusinessFailure bool `json:"hasBusinessFailure"`
	// NewToken is the renewed session JWT. It is a secret.
	NewToken *string `json:"newToken,omitempty"`
}

// IsBusinessFailure reports whether success is false or errorCode is non-zero.
func (r *ValidateResponse) IsBusinessFailure() bool { return isFailure(r.Success, int32(r.ErrorCode)) }

func (r *ValidateResponse) envelope() envelope {
	return envelope{r.Success, int32(r.ErrorCode), r.ErrorCode.String(), r.ErrorMessage}
}

// SearchContractResponse is the Contract_SearchContracts response.
// Missing arrays decode as nil slices.
type SearchContractResponse struct {
	// Success is false for a business failure.
	Success bool `json:"success"`
	// ErrorCode is zero on success.
	ErrorCode SearchContractErrorCode `json:"errorCode"`
	// ErrorMessage describes a failure; the gateway may leave it null.
	ErrorMessage *string `json:"errorMessage,omitempty"`
	// HasBusinessFailure is the gateway's own failure flag; it decodes as
	// false when absent. Failures are detected from Success and ErrorCode.
	HasBusinessFailure bool `json:"hasBusinessFailure"`
	// Contracts are the matching contracts (at most 20).
	Contracts []Contract `json:"contracts,omitempty"`
}

// IsBusinessFailure reports whether success is false or errorCode is non-zero.
func (r *SearchContractResponse) IsBusinessFailure() bool {
	return isFailure(r.Success, int32(r.ErrorCode))
}

func (r *SearchContractResponse) envelope() envelope {
	return envelope{r.Success, int32(r.ErrorCode), r.ErrorCode.String(), r.ErrorMessage}
}

// SearchContractByIDResponse is the Contract_SearchContractById response (spec schema SearchContractByIdResponse).
type SearchContractByIDResponse struct {
	// Success is false for a business failure.
	Success bool `json:"success"`
	// ErrorCode is zero on success.
	ErrorCode SearchContractByIDErrorCode `json:"errorCode"`
	// ErrorMessage describes a failure; the gateway may leave it null.
	ErrorMessage *string `json:"errorMessage,omitempty"`
	// HasBusinessFailure is the gateway's own failure flag; it decodes as
	// false when absent. Failures are detected from Success and ErrorCode.
	HasBusinessFailure bool `json:"hasBusinessFailure"`
	// Contract is the requested contract.
	Contract *Contract `json:"contract,omitempty"`
}

// IsBusinessFailure reports whether success is false or errorCode is non-zero.
func (r *SearchContractByIDResponse) IsBusinessFailure() bool {
	return isFailure(r.Success, int32(r.ErrorCode))
}

func (r *SearchContractByIDResponse) envelope() envelope {
	return envelope{r.Success, int32(r.ErrorCode), r.ErrorCode.String(), r.ErrorMessage}
}

// ListAvailableContractResponse is the Contract_AvailableContracts response.
// Missing arrays decode as nil slices.
type ListAvailableContractResponse struct {
	// Success is false for a business failure.
	Success bool `json:"success"`
	// ErrorCode is zero on success.
	ErrorCode ListAvailableContractErrorCode `json:"errorCode"`
	// ErrorMessage describes a failure; the gateway may leave it null.
	ErrorMessage *string `json:"errorMessage,omitempty"`
	// HasBusinessFailure is the gateway's own failure flag; it decodes as
	// false when absent. Failures are detected from Success and ErrorCode.
	HasBusinessFailure bool `json:"hasBusinessFailure"`
	// Contracts are the available contracts.
	Contracts []Contract `json:"contracts,omitempty"`
}

// IsBusinessFailure reports whether success is false or errorCode is non-zero.
func (r *ListAvailableContractResponse) IsBusinessFailure() bool {
	return isFailure(r.Success, int32(r.ErrorCode))
}

func (r *ListAvailableContractResponse) envelope() envelope {
	return envelope{r.Success, int32(r.ErrorCode), r.ErrorCode.String(), r.ErrorMessage}
}

// RetrieveBarResponse is the History_GetBars response.
// Missing arrays decode as nil slices.
type RetrieveBarResponse struct {
	// Success is false for a business failure.
	Success bool `json:"success"`
	// ErrorCode is zero on success.
	ErrorCode RetrieveBarErrorCode `json:"errorCode"`
	// ErrorMessage describes a failure; the gateway may leave it null.
	ErrorMessage *string `json:"errorMessage,omitempty"`
	// HasBusinessFailure is the gateway's own failure flag; it decodes as
	// false when absent. Failures are detected from Success and ErrorCode.
	HasBusinessFailure bool `json:"hasBusinessFailure"`
	// Bars are the requested bars.
	Bars []AggregateBar `json:"bars,omitempty"`
}

// IsBusinessFailure reports whether success is false or errorCode is non-zero.
func (r *RetrieveBarResponse) IsBusinessFailure() bool {
	return isFailure(r.Success, int32(r.ErrorCode))
}

func (r *RetrieveBarResponse) envelope() envelope {
	return envelope{r.Success, int32(r.ErrorCode), r.ErrorCode.String(), r.ErrorMessage}
}

// SearchOrderResponse is the Order_SearchOrders and Order_SearchOpenOrders response.
// Missing arrays decode as nil slices.
type SearchOrderResponse struct {
	// Success is false for a business failure.
	Success bool `json:"success"`
	// ErrorCode is zero on success.
	ErrorCode SearchOrderErrorCode `json:"errorCode"`
	// ErrorMessage describes a failure; the gateway may leave it null.
	ErrorMessage *string `json:"errorMessage,omitempty"`
	// HasBusinessFailure is the gateway's own failure flag; it decodes as
	// false when absent. Failures are detected from Success and ErrorCode.
	HasBusinessFailure bool `json:"hasBusinessFailure"`
	// Orders are the matching orders.
	Orders []Order `json:"orders,omitempty"`
}

// IsBusinessFailure reports whether success is false or errorCode is non-zero.
func (r *SearchOrderResponse) IsBusinessFailure() bool {
	return isFailure(r.Success, int32(r.ErrorCode))
}

func (r *SearchOrderResponse) envelope() envelope {
	return envelope{r.Success, int32(r.ErrorCode), r.ErrorCode.String(), r.ErrorMessage}
}

// SearchOrderByIDResponse is the Order_SearchOrderById response (spec schema SearchOrderByIdResponse).
type SearchOrderByIDResponse struct {
	// Success is false for a business failure.
	Success bool `json:"success"`
	// ErrorCode is zero on success.
	ErrorCode SearchOrderByIDErrorCode `json:"errorCode"`
	// ErrorMessage describes a failure; the gateway may leave it null.
	ErrorMessage *string `json:"errorMessage,omitempty"`
	// HasBusinessFailure is the gateway's own failure flag; it decodes as
	// false when absent. Failures are detected from Success and ErrorCode.
	HasBusinessFailure bool `json:"hasBusinessFailure"`
	// Order is the requested order.
	Order *Order `json:"order,omitempty"`
}

// IsBusinessFailure reports whether success is false or errorCode is non-zero.
func (r *SearchOrderByIDResponse) IsBusinessFailure() bool {
	return isFailure(r.Success, int32(r.ErrorCode))
}

func (r *SearchOrderByIDResponse) envelope() envelope {
	return envelope{r.Success, int32(r.ErrorCode), r.ErrorCode.String(), r.ErrorMessage}
}

// SearchOrdersQueryResponse is the Order_SearchOrdersQuery response.
// Missing arrays decode as nil slices.
type SearchOrdersQueryResponse struct {
	// Success is false for a business failure.
	Success bool `json:"success"`
	// ErrorCode is zero on success.
	ErrorCode SearchOrderErrorCode `json:"errorCode"`
	// ErrorMessage describes a failure; the gateway may leave it null.
	ErrorMessage *string `json:"errorMessage,omitempty"`
	// HasBusinessFailure is the gateway's own failure flag; it decodes as
	// false when absent. Failures are detected from Success and ErrorCode.
	HasBusinessFailure bool `json:"hasBusinessFailure"`
	// Orders are the orders of the requested page.
	Orders []Order `json:"orders,omitempty"`
	// TotalCount is the number of matching orders, when requested.
	TotalCount *int32 `json:"totalCount,omitempty"`
}

// IsBusinessFailure reports whether success is false or errorCode is non-zero.
func (r *SearchOrdersQueryResponse) IsBusinessFailure() bool {
	return isFailure(r.Success, int32(r.ErrorCode))
}

func (r *SearchOrdersQueryResponse) envelope() envelope {
	return envelope{r.Success, int32(r.ErrorCode), r.ErrorCode.String(), r.ErrorMessage}
}

// PlaceOrderResponse is the Order_PlaceOrder response.
type PlaceOrderResponse struct {
	// Success is false for a business failure.
	Success bool `json:"success"`
	// ErrorCode is zero on success.
	ErrorCode PlaceOrderErrorCode `json:"errorCode"`
	// ErrorMessage describes a failure; the gateway may leave it null.
	ErrorMessage *string `json:"errorMessage,omitempty"`
	// HasBusinessFailure is the gateway's own failure flag; it decodes as
	// false when absent. Failures are detected from Success and ErrorCode.
	HasBusinessFailure bool `json:"hasBusinessFailure"`
	// OrderID is the order identifier. It can be present on a business
	// failure too; an identifier does not establish acceptance.
	OrderID *int64 `json:"orderId,omitempty"`
}

// IsBusinessFailure reports whether success is false or errorCode is non-zero.
func (r *PlaceOrderResponse) IsBusinessFailure() bool {
	return isFailure(r.Success, int32(r.ErrorCode))
}

func (r *PlaceOrderResponse) envelope() envelope {
	return envelope{r.Success, int32(r.ErrorCode), r.ErrorCode.String(), r.ErrorMessage}
}

// CancelOrderResponse is the Order_CancelOrder response.
type CancelOrderResponse struct {
	// Success is false for a business failure.
	Success bool `json:"success"`
	// ErrorCode is zero on success.
	ErrorCode CancelOrderErrorCode `json:"errorCode"`
	// ErrorMessage describes a failure; the gateway may leave it null.
	ErrorMessage *string `json:"errorMessage,omitempty"`
	// HasBusinessFailure is the gateway's own failure flag; it decodes as
	// false when absent. Failures are detected from Success and ErrorCode.
	HasBusinessFailure bool `json:"hasBusinessFailure"`
}

// IsBusinessFailure reports whether success is false or errorCode is non-zero.
func (r *CancelOrderResponse) IsBusinessFailure() bool {
	return isFailure(r.Success, int32(r.ErrorCode))
}

func (r *CancelOrderResponse) envelope() envelope {
	return envelope{r.Success, int32(r.ErrorCode), r.ErrorCode.String(), r.ErrorMessage}
}

// ModifyOrderResponse is the Order_ModifyOrder response.
type ModifyOrderResponse struct {
	// Success is false for a business failure.
	Success bool `json:"success"`
	// ErrorCode is zero on success.
	ErrorCode ModifyOrderErrorCode `json:"errorCode"`
	// ErrorMessage describes a failure; the gateway may leave it null.
	ErrorMessage *string `json:"errorMessage,omitempty"`
	// HasBusinessFailure is the gateway's own failure flag; it decodes as
	// false when absent. Failures are detected from Success and ErrorCode.
	HasBusinessFailure bool `json:"hasBusinessFailure"`
}

// IsBusinessFailure reports whether success is false or errorCode is non-zero.
func (r *ModifyOrderResponse) IsBusinessFailure() bool {
	return isFailure(r.Success, int32(r.ErrorCode))
}

func (r *ModifyOrderResponse) envelope() envelope {
	return envelope{r.Success, int32(r.ErrorCode), r.ErrorCode.String(), r.ErrorMessage}
}

// SearchPositionResponse is the Position_SearchOpenPositions response.
// Missing arrays decode as nil slices.
type SearchPositionResponse struct {
	// Success is false for a business failure.
	Success bool `json:"success"`
	// ErrorCode is zero on success.
	ErrorCode SearchPositionErrorCode `json:"errorCode"`
	// ErrorMessage describes a failure; the gateway may leave it null.
	ErrorMessage *string `json:"errorMessage,omitempty"`
	// HasBusinessFailure is the gateway's own failure flag; it decodes as
	// false when absent. Failures are detected from Success and ErrorCode.
	HasBusinessFailure bool `json:"hasBusinessFailure"`
	// Positions are the open positions.
	Positions []Position `json:"positions,omitempty"`
}

// IsBusinessFailure reports whether success is false or errorCode is non-zero.
func (r *SearchPositionResponse) IsBusinessFailure() bool {
	return isFailure(r.Success, int32(r.ErrorCode))
}

func (r *SearchPositionResponse) envelope() envelope {
	return envelope{r.Success, int32(r.ErrorCode), r.ErrorCode.String(), r.ErrorMessage}
}

// ClosePositionResponse is the Position_CloseContractPosition response.
type ClosePositionResponse struct {
	// Success is false for a business failure.
	Success bool `json:"success"`
	// ErrorCode is zero on success.
	ErrorCode ClosePositionErrorCode `json:"errorCode"`
	// ErrorMessage describes a failure; the gateway may leave it null.
	ErrorMessage *string `json:"errorMessage,omitempty"`
	// HasBusinessFailure is the gateway's own failure flag; it decodes as
	// false when absent. Failures are detected from Success and ErrorCode.
	HasBusinessFailure bool `json:"hasBusinessFailure"`
}

// IsBusinessFailure reports whether success is false or errorCode is non-zero.
func (r *ClosePositionResponse) IsBusinessFailure() bool {
	return isFailure(r.Success, int32(r.ErrorCode))
}

func (r *ClosePositionResponse) envelope() envelope {
	return envelope{r.Success, int32(r.ErrorCode), r.ErrorCode.String(), r.ErrorMessage}
}

// PartialClosePositionResponse is the Position_PartialCloseContractPosition response.
type PartialClosePositionResponse struct {
	// Success is false for a business failure.
	Success bool `json:"success"`
	// ErrorCode is zero on success.
	ErrorCode PartialClosePositionErrorCode `json:"errorCode"`
	// ErrorMessage describes a failure; the gateway may leave it null.
	ErrorMessage *string `json:"errorMessage,omitempty"`
	// HasBusinessFailure is the gateway's own failure flag; it decodes as
	// false when absent. Failures are detected from Success and ErrorCode.
	HasBusinessFailure bool `json:"hasBusinessFailure"`
}

// IsBusinessFailure reports whether success is false or errorCode is non-zero.
func (r *PartialClosePositionResponse) IsBusinessFailure() bool {
	return isFailure(r.Success, int32(r.ErrorCode))
}

func (r *PartialClosePositionResponse) envelope() envelope {
	return envelope{r.Success, int32(r.ErrorCode), r.ErrorCode.String(), r.ErrorMessage}
}

// SearchHalfTradeResponse is the Trade_SearchHalfTurnTrades response.
// Missing arrays decode as nil slices.
type SearchHalfTradeResponse struct {
	// Success is false for a business failure.
	Success bool `json:"success"`
	// ErrorCode is zero on success.
	ErrorCode SearchTradeErrorCode `json:"errorCode"`
	// ErrorMessage describes a failure; the gateway may leave it null.
	ErrorMessage *string `json:"errorMessage,omitempty"`
	// HasBusinessFailure is the gateway's own failure flag; it decodes as
	// false when absent. Failures are detected from Success and ErrorCode.
	HasBusinessFailure bool `json:"hasBusinessFailure"`
	// Trades are the matching trades. A nil ProfitAndLoss marks a half-turn.
	Trades []HalfTrade `json:"trades,omitempty"`
}

// IsBusinessFailure reports whether success is false or errorCode is non-zero.
func (r *SearchHalfTradeResponse) IsBusinessFailure() bool {
	return isFailure(r.Success, int32(r.ErrorCode))
}

func (r *SearchHalfTradeResponse) envelope() envelope {
	return envelope{r.Success, int32(r.ErrorCode), r.ErrorCode.String(), r.ErrorMessage}
}
func optionalString(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%q", *p)
}

func redactedPresence(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return redacted
}

// String returns a representation with the token redacted.
func (r LoginResponse) String() string {
	return fmt.Sprintf("LoginResponse{Success: %t, ErrorCode: %s, ErrorMessage: %s, HasBusinessFailure: %t, Token: %s}",
		r.Success, r.ErrorCode, optionalString(r.ErrorMessage), r.HasBusinessFailure, redactedPresence(r.Token))
}

// GoString returns a Go-syntax representation with the token redacted.
func (r LoginResponse) GoString() string {
	return "topstepx." + r.String()
}

// Format implements fmt.Formatter so that no verb prints the token.
func (r LoginResponse) Format(f fmt.State, verb rune) { formatRedacted(f, verb, r) }

// LogValue implements slog.LogValuer with the token redacted.
func (r LoginResponse) LogValue() slog.Value {
	return slog.GroupValue(slog.Bool("success", r.Success), slog.Int("errorCode", int(r.ErrorCode)),
		slog.String("errorMessage", optionalString(r.ErrorMessage)), slog.String("token", redactedPresence(r.Token)))
}

// String returns a representation with the token redacted.
func (r ValidateResponse) String() string {
	return fmt.Sprintf("ValidateResponse{Success: %t, ErrorCode: %s, ErrorMessage: %s, HasBusinessFailure: %t, NewToken: %s}",
		r.Success, r.ErrorCode, optionalString(r.ErrorMessage), r.HasBusinessFailure, redactedPresence(r.NewToken))
}

// GoString returns a Go-syntax representation with the token redacted.
func (r ValidateResponse) GoString() string {
	return "topstepx." + r.String()
}

// Format implements fmt.Formatter so that no verb prints the token.
func (r ValidateResponse) Format(f fmt.State, verb rune) { formatRedacted(f, verb, r) }

// LogValue implements slog.LogValuer with the token redacted.
func (r ValidateResponse) LogValue() slog.Value {
	return slog.GroupValue(slog.Bool("success", r.Success), slog.Int("errorCode", int(r.ErrorCode)),
		slog.String("errorMessage", optionalString(r.ErrorMessage)), slog.String("newToken", redactedPresence(r.NewToken)))
}
