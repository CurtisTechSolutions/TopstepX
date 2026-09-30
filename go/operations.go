package topstepx

import "net/http"

// OperationID is the spec operationId of a REST operation. Pass it to
// Client.Do.
type OperationID string

// The 21 REST operations of the ProjectX Gateway API.
const (
	OpAccountSearchAccounts                OperationID = "Account_SearchAccounts"
	OpAuthLoginApp                         OperationID = "Auth_LoginApp"
	OpAuthLoginKey                         OperationID = "Auth_LoginKey"
	OpAuthLogout                           OperationID = "Auth_Logout"
	OpAuthValidate                         OperationID = "Auth_Validate"
	OpContractSearchContracts              OperationID = "Contract_SearchContracts"
	OpContractSearchContractByID           OperationID = "Contract_SearchContractById"
	OpContractAvailableContracts           OperationID = "Contract_AvailableContracts"
	OpHistoryGetBars                       OperationID = "History_GetBars"
	OpOrderSearchOrders                    OperationID = "Order_SearchOrders"
	OpOrderSearchOrderByID                 OperationID = "Order_SearchOrderById"
	OpOrderSearchOpenOrders                OperationID = "Order_SearchOpenOrders"
	OpOrderSearchOrdersQuery               OperationID = "Order_SearchOrdersQuery"
	OpOrderPlaceOrder                      OperationID = "Order_PlaceOrder"
	OpOrderCancelOrder                     OperationID = "Order_CancelOrder"
	OpOrderModifyOrder                     OperationID = "Order_ModifyOrder"
	OpPositionSearchOpenPositions          OperationID = "Position_SearchOpenPositions"
	OpPositionCloseContractPosition        OperationID = "Position_CloseContractPosition"
	OpPositionPartialCloseContractPosition OperationID = "Position_PartialCloseContractPosition"
	OpStatusPing                           OperationID = "Status_Ping"
	OpTradeSearchHalfTurnTrades            OperationID = "Trade_SearchHalfTurnTrades"
)

// AuthMode says how an operation authenticates.
type AuthMode int

// AuthMode values.
const (
	// AuthNone sends no Authorization header (the login operations).
	AuthNone AuthMode = iota
	// AuthBearer requires a bearer token, obtained and renewed automatically.
	AuthBearer
	// AuthBearerIfAvailable attaches a bearer token when one is held or
	// obtainable and otherwise sends the request anonymously (Status_Ping).
	AuthBearerIfAvailable
)

// String returns the mode's name.
func (m AuthMode) String() string {
	switch m {
	case AuthNone:
		return "none"
	case AuthBearer:
		return "bearer"
	case AuthBearerIfAvailable:
		return "bearer-if-available"
	}
	return "unknown"
}

// RateBucket is the client-side rate-limit bucket of an operation.
type RateBucket int

// RateBucket values.
const (
	// RateBucketGeneral covers every operation except History_GetBars.
	RateBucketGeneral RateBucket = iota
	// RateBucketHistory covers History_GetBars.
	RateBucketHistory
)

// String returns the bucket's name.
func (b RateBucket) String() string {
	switch b {
	case RateBucketGeneral:
		return "general"
	case RateBucketHistory:
		return "history"
	}
	return "unknown"
}

// RetryClass decides which failures an operation retries.
type RetryClass int

// RetryClass values.
const (
	// RetryRead operations retry on HTTP 429, 502, 503, 504 and transport
	// errors.
	RetryRead RetryClass = iota
	// RetryWrite operations retry only on HTTP 429, because any other failure
	// may follow a request that already executed.
	RetryWrite
)

// String returns the class's name.
func (c RetryClass) String() string {
	switch c {
	case RetryRead:
		return "read"
	case RetryWrite:
		return "write"
	}
	return "unknown"
}

// OperationInfo describes one REST operation.
type OperationInfo struct {
	// ID is the spec operationId.
	ID OperationID
	// Method is the HTTP method.
	Method string
	// Path is the request path, relative to the base URL.
	Path string
	// Auth is the authentication mode.
	Auth AuthMode
	// RateBucket is the client-side rate-limit bucket.
	RateBucket RateBucket
	// RetryClass decides which failures are retried.
	RetryClass RetryClass
	// HasBody reports whether the operation sends a JSON request body.
	// Auth_Validate and Auth_Logout are POSTs with an empty body.
	HasBody bool
}

var operationTable = []OperationInfo{
	{OpAccountSearchAccounts, http.MethodPost, "/api/Account/search", AuthBearer, RateBucketGeneral, RetryRead, true},
	{OpAuthLoginApp, http.MethodPost, "/api/Auth/loginApp", AuthNone, RateBucketGeneral, RetryWrite, true},
	{OpAuthLoginKey, http.MethodPost, "/api/Auth/loginKey", AuthNone, RateBucketGeneral, RetryWrite, true},
	{OpAuthLogout, http.MethodPost, "/api/Auth/logout", AuthBearer, RateBucketGeneral, RetryWrite, false},
	{OpAuthValidate, http.MethodPost, "/api/Auth/validate", AuthBearer, RateBucketGeneral, RetryWrite, false},
	{OpContractSearchContracts, http.MethodPost, "/api/Contract/search", AuthBearer, RateBucketGeneral, RetryRead, true},
	{OpContractSearchContractByID, http.MethodPost, "/api/Contract/searchById", AuthBearer, RateBucketGeneral, RetryRead, true},
	{OpContractAvailableContracts, http.MethodPost, "/api/Contract/available", AuthBearer, RateBucketGeneral, RetryRead, true},
	{OpHistoryGetBars, http.MethodPost, "/api/History/retrieveBars", AuthBearer, RateBucketHistory, RetryRead, true},
	{OpOrderSearchOrders, http.MethodPost, "/api/Order/search", AuthBearer, RateBucketGeneral, RetryRead, true},
	{OpOrderSearchOrderByID, http.MethodPost, "/api/Order/searchById", AuthBearer, RateBucketGeneral, RetryRead, true},
	{OpOrderSearchOpenOrders, http.MethodPost, "/api/Order/searchOpen", AuthBearer, RateBucketGeneral, RetryRead, true},
	{OpOrderSearchOrdersQuery, http.MethodPost, "/api/Order/v2/query", AuthBearer, RateBucketGeneral, RetryRead, true},
	{OpOrderPlaceOrder, http.MethodPost, "/api/Order/place", AuthBearer, RateBucketGeneral, RetryWrite, true},
	{OpOrderCancelOrder, http.MethodPost, "/api/Order/cancel", AuthBearer, RateBucketGeneral, RetryWrite, true},
	{OpOrderModifyOrder, http.MethodPost, "/api/Order/modify", AuthBearer, RateBucketGeneral, RetryWrite, true},
	{OpPositionSearchOpenPositions, http.MethodPost, "/api/Position/searchOpen", AuthBearer, RateBucketGeneral, RetryRead, true},
	{OpPositionCloseContractPosition, http.MethodPost, "/api/Position/closeContract", AuthBearer, RateBucketGeneral, RetryWrite, true},
	{OpPositionPartialCloseContractPosition, http.MethodPost, "/api/Position/partialCloseContract", AuthBearer, RateBucketGeneral, RetryWrite, true},
	{OpStatusPing, http.MethodGet, "/api/Status/ping", AuthBearerIfAvailable, RateBucketGeneral, RetryRead, false},
	{OpTradeSearchHalfTurnTrades, http.MethodPost, "/api/Trade/search", AuthBearer, RateBucketGeneral, RetryRead, true},
}

var operationIndex = func() map[OperationID]OperationInfo {
	m := make(map[OperationID]OperationInfo, len(operationTable))
	for _, op := range operationTable {
		m[op.ID] = op
	}
	return m
}()

// Operations returns the descriptions of all 21 operations.
func Operations() []OperationInfo {
	out := make([]OperationInfo, len(operationTable))
	copy(out, operationTable)
	return out
}

// Info returns the description of the operation.
func (id OperationID) Info() (OperationInfo, bool) {
	info, ok := operationIndex[id]
	return info, ok
}

// String returns the operationId.
func (id OperationID) String() string { return string(id) }
