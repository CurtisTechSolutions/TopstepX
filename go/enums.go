package topstepx

import "strconv"

// enumString returns names[v], or "Unrecognized(v)" when the spec does not
// list v. All spec enums are dense and start at zero.
func enumString(names []string, v int32) string {
	if v >= 0 && int(v) < len(names) {
		return names[v]
	}
	return "Unrecognized(" + strconv.FormatInt(int64(v), 10) + ")"
}

// enumKnown reports whether v is listed by the spec.
func enumKnown(names []string, v int32) bool {
	return v >= 0 && int(v) < len(names)
}

// AggregateBarUnit is the aggregation unit of historical bars (spec schema
// AggregateBarUnit). The guide documents Second through Month; Unspecified
// and Tick appear only in Swagger and their acceptance is unverified.
type AggregateBarUnit int32

// AggregateBarUnit values.
const (
	AggregateBarUnitUnspecified AggregateBarUnit = 0
	AggregateBarUnitSecond      AggregateBarUnit = 1
	AggregateBarUnitMinute      AggregateBarUnit = 2
	AggregateBarUnitHour        AggregateBarUnit = 3
	AggregateBarUnitDay         AggregateBarUnit = 4
	AggregateBarUnitWeek        AggregateBarUnit = 5
	AggregateBarUnitMonth       AggregateBarUnit = 6
	AggregateBarUnitTick        AggregateBarUnit = 7
)

var aggregateBarUnitNames = []string{"Unspecified", "Second", "Minute", "Hour", "Day", "Week", "Month", "Tick"}

// String returns the spec name of the value, or "Unrecognized(<n>)" for a
// value the spec does not list.
func (v AggregateBarUnit) String() string { return enumString(aggregateBarUnitNames, int32(v)) }

// IsKnown reports whether the spec lists the value.
func (v AggregateBarUnit) IsKnown() bool { return enumKnown(aggregateBarUnitNames, int32(v)) }

// OrderStatus is the lifecycle status of an order (spec schema OrderStatus).
type OrderStatus int32

// OrderStatus values.
const (
	OrderStatusNone                OrderStatus = 0
	OrderStatusOpen                OrderStatus = 1
	OrderStatusFilled              OrderStatus = 2
	OrderStatusCancelled           OrderStatus = 3
	OrderStatusExpired             OrderStatus = 4
	OrderStatusRejected            OrderStatus = 5
	OrderStatusPending             OrderStatus = 6
	OrderStatusPendingCancellation OrderStatus = 7
	OrderStatusSuspended           OrderStatus = 8
)

var orderStatusNames = []string{"None", "Open", "Filled", "Cancelled", "Expired", "Rejected", "Pending", "PendingCancellation", "Suspended"}

// String returns the spec name of the value, or "Unrecognized(<n>)" for a
// value the spec does not list.
func (v OrderStatus) String() string { return enumString(orderStatusNames, int32(v)) }

// IsKnown reports whether the spec lists the value.
func (v OrderStatus) IsKnown() bool { return enumKnown(orderStatusNames, int32(v)) }

// OrderType is the type of an order (spec schema OrderType). Placement and
// brackets accept Limit, Market, Stop, TrailingStop, JoinBid and JoinAsk;
// Unknown and StopLimit appear only in responses.
type OrderType int32

// OrderType values.
const (
	OrderTypeUnknown      OrderType = 0
	OrderTypeLimit        OrderType = 1
	OrderTypeMarket       OrderType = 2
	OrderTypeStopLimit    OrderType = 3
	OrderTypeStop         OrderType = 4
	OrderTypeTrailingStop OrderType = 5
	OrderTypeJoinBid      OrderType = 6
	OrderTypeJoinAsk      OrderType = 7
)

var orderTypeNames = []string{"Unknown", "Limit", "Market", "StopLimit", "Stop", "TrailingStop", "JoinBid", "JoinAsk"}

// String returns the spec name of the value, or "Unrecognized(<n>)" for a
// value the spec does not list.
func (v OrderType) String() string { return enumString(orderTypeNames, int32(v)) }

// IsKnown reports whether the spec lists the value.
func (v OrderType) IsKnown() bool { return enumKnown(orderTypeNames, int32(v)) }

// OrderSide is the side of an order or trade (spec schema OrderSide): Bid buys
// and Ask sells.
type OrderSide int32

// OrderSide values.
const (
	OrderSideBid OrderSide = 0
	OrderSideAsk OrderSide = 1
)

var orderSideNames = []string{"Bid", "Ask"}

// String returns the spec name of the value, or "Unrecognized(<n>)" for a
// value the spec does not list.
func (v OrderSide) String() string { return enumString(orderSideNames, int32(v)) }

// IsKnown reports whether the spec lists the value.
func (v OrderSide) IsKnown() bool { return enumKnown(orderSideNames, int32(v)) }

// OrderSortField selects the sort key of Order_SearchOrdersQuery (spec schema
// OrderSortField).
type OrderSortField int32

// OrderSortField values.
const (
	OrderSortFieldCreatedAt OrderSortField = 0
	OrderSortFieldID        OrderSortField = 1
)

var orderSortFieldNames = []string{"CreatedAt", "Id"}

// String returns the spec name of the value, or "Unrecognized(<n>)" for a
// value the spec does not list.
func (v OrderSortField) String() string { return enumString(orderSortFieldNames, int32(v)) }

// IsKnown reports whether the spec lists the value.
func (v OrderSortField) IsKnown() bool { return enumKnown(orderSortFieldNames, int32(v)) }

// SortDirection selects the sort order of Order_SearchOrdersQuery (spec schema
// SortDirection).
type SortDirection int32

// SortDirection values.
const (
	SortDirectionAsc  SortDirection = 0
	SortDirectionDesc SortDirection = 1
)

var sortDirectionNames = []string{"Asc", "Desc"}

// String returns the spec name of the value, or "Unrecognized(<n>)" for a
// value the spec does not list.
func (v SortDirection) String() string { return enumString(sortDirectionNames, int32(v)) }

// IsKnown reports whether the spec lists the value.
func (v SortDirection) IsKnown() bool { return enumKnown(sortDirectionNames, int32(v)) }

// PositionType is the direction of a position (spec schema PositionType).
type PositionType int32

// PositionType values.
const (
	PositionTypeUndefined PositionType = 0
	PositionTypeLong      PositionType = 1
	PositionTypeShort     PositionType = 2
)

var positionTypeNames = []string{"Undefined", "Long", "Short"}

// String returns the spec name of the value, or "Unrecognized(<n>)" for a
// value the spec does not list.
func (v PositionType) String() string { return enumString(positionTypeNames, int32(v)) }

// IsKnown reports whether the spec lists the value.
func (v PositionType) IsKnown() bool { return enumKnown(positionTypeNames, int32(v)) }

// SearchAccountErrorCode is the errorCode of Account_SearchAccounts responses
// (spec schema SearchAccountErrorCode). Zero means success.
type SearchAccountErrorCode int32

// SearchAccountErrorCode values.
const (
	SearchAccountErrorCodeSuccess SearchAccountErrorCode = 0
)

var searchAccountErrorCodeNames = []string{"Success"}

// String returns the spec name of the value, or "Unrecognized(<n>)" for a
// value the spec does not list.
func (v SearchAccountErrorCode) String() string {
	return enumString(searchAccountErrorCodeNames, int32(v))
}

// IsKnown reports whether the spec lists the value.
func (v SearchAccountErrorCode) IsKnown() bool {
	return enumKnown(searchAccountErrorCodeNames, int32(v))
}

// LoginErrorCode is the errorCode of Auth_LoginKey and Auth_LoginApp responses
// (spec schema LoginErrorCode). Zero means success.
type LoginErrorCode int32

// LoginErrorCode values.
const (
	LoginErrorCodeSuccess                      LoginErrorCode = 0
	LoginErrorCodeUserNotFound                 LoginErrorCode = 1
	LoginErrorCodePasswordVerificationFailed   LoginErrorCode = 2
	LoginErrorCodeInvalidCredentials           LoginErrorCode = 3
	LoginErrorCodeAppNotFound                  LoginErrorCode = 4
	LoginErrorCodeAppVerificationFailed        LoginErrorCode = 5
	LoginErrorCodeInvalidDevice                LoginErrorCode = 6
	LoginErrorCodeAgreementsNotSigned          LoginErrorCode = 7
	LoginErrorCodeUnknownError                 LoginErrorCode = 8
	LoginErrorCodeAPISubscriptionNotFound      LoginErrorCode = 9
	LoginErrorCodeAPIKeyAuthenticationDisabled LoginErrorCode = 10
)

var loginErrorCodeNames = []string{"Success", "UserNotFound", "PasswordVerificationFailed", "InvalidCredentials", "AppNotFound", "AppVerificationFailed", "InvalidDevice", "AgreementsNotSigned", "UnknownError", "ApiSubscriptionNotFound", "ApiKeyAuthenticationDisabled"}

// String returns the spec name of the value, or "Unrecognized(<n>)" for a
// value the spec does not list.
func (v LoginErrorCode) String() string { return enumString(loginErrorCodeNames, int32(v)) }

// IsKnown reports whether the spec lists the value.
func (v LoginErrorCode) IsKnown() bool { return enumKnown(loginErrorCodeNames, int32(v)) }

// LogoutErrorCode is the errorCode of Auth_Logout responses (spec schema
// LogoutErrorCode). Zero means success.
type LogoutErrorCode int32

// LogoutErrorCode values.
const (
	LogoutErrorCodeSuccess        LogoutErrorCode = 0
	LogoutErrorCodeInvalidSession LogoutErrorCode = 1
	LogoutErrorCodeUnknownError   LogoutErrorCode = 2
)

var logoutErrorCodeNames = []string{"Success", "InvalidSession", "UnknownError"}

// String returns the spec name of the value, or "Unrecognized(<n>)" for a
// value the spec does not list.
func (v LogoutErrorCode) String() string { return enumString(logoutErrorCodeNames, int32(v)) }

// IsKnown reports whether the spec lists the value.
func (v LogoutErrorCode) IsKnown() bool { return enumKnown(logoutErrorCodeNames, int32(v)) }

// ValidateErrorCode is the errorCode of Auth_Validate responses (spec schema
// ValidateErrorCode). Zero means success.
type ValidateErrorCode int32

// ValidateErrorCode values.
const (
	ValidateErrorCodeSuccess         ValidateErrorCode = 0
	ValidateErrorCodeInvalidSession  ValidateErrorCode = 1
	ValidateErrorCodeSessionNotFound ValidateErrorCode = 2
	ValidateErrorCodeExpiredToken    ValidateErrorCode = 3
	ValidateErrorCodeUnknownError    ValidateErrorCode = 4
)

var validateErrorCodeNames = []string{"Success", "InvalidSession", "SessionNotFound", "ExpiredToken", "UnknownError"}

// String returns the spec name of the value, or "Unrecognized(<n>)" for a
// value the spec does not list.
func (v ValidateErrorCode) String() string { return enumString(validateErrorCodeNames, int32(v)) }

// IsKnown reports whether the spec lists the value.
func (v ValidateErrorCode) IsKnown() bool { return enumKnown(validateErrorCodeNames, int32(v)) }

// SearchContractErrorCode is the errorCode of Contract_SearchContracts
// responses (spec schema SearchContractErrorCode). Zero means success.
type SearchContractErrorCode int32

// SearchContractErrorCode values.
const (
	SearchContractErrorCodeSuccess SearchContractErrorCode = 0
)

var searchContractErrorCodeNames = []string{"Success"}

// String returns the spec name of the value, or "Unrecognized(<n>)" for a
// value the spec does not list.
func (v SearchContractErrorCode) String() string {
	return enumString(searchContractErrorCodeNames, int32(v))
}

// IsKnown reports whether the spec lists the value.
func (v SearchContractErrorCode) IsKnown() bool {
	return enumKnown(searchContractErrorCodeNames, int32(v))
}

// SearchContractByIDErrorCode is the errorCode of Contract_SearchContractById
// responses (spec schema SearchContractByIdErrorCode). Zero means success.
type SearchContractByIDErrorCode int32

// SearchContractByIDErrorCode values.
const (
	SearchContractByIDErrorCodeSuccess          SearchContractByIDErrorCode = 0
	SearchContractByIDErrorCodeContractNotFound SearchContractByIDErrorCode = 1
)

var searchContractByIDErrorCodeNames = []string{"Success", "ContractNotFound"}

// String returns the spec name of the value, or "Unrecognized(<n>)" for a
// value the spec does not list.
func (v SearchContractByIDErrorCode) String() string {
	return enumString(searchContractByIDErrorCodeNames, int32(v))
}

// IsKnown reports whether the spec lists the value.
func (v SearchContractByIDErrorCode) IsKnown() bool {
	return enumKnown(searchContractByIDErrorCodeNames, int32(v))
}

// ListAvailableContractErrorCode is the errorCode of
// Contract_AvailableContracts responses (spec schema
// ListAvailableContractErrorCode). Zero means success.
type ListAvailableContractErrorCode int32

// ListAvailableContractErrorCode values.
const (
	ListAvailableContractErrorCodeSuccess ListAvailableContractErrorCode = 0
)

var listAvailableContractErrorCodeNames = []string{"Success"}

// String returns the spec name of the value, or "Unrecognized(<n>)" for a
// value the spec does not list.
func (v ListAvailableContractErrorCode) String() string {
	return enumString(listAvailableContractErrorCodeNames, int32(v))
}

// IsKnown reports whether the spec lists the value.
func (v ListAvailableContractErrorCode) IsKnown() bool {
	return enumKnown(listAvailableContractErrorCodeNames, int32(v))
}

// RetrieveBarErrorCode is the errorCode of History_GetBars responses (spec
// schema RetrieveBarErrorCode). Zero means success.
type RetrieveBarErrorCode int32

// RetrieveBarErrorCode values.
const (
	RetrieveBarErrorCodeSuccess           RetrieveBarErrorCode = 0
	RetrieveBarErrorCodeContractNotFound  RetrieveBarErrorCode = 1
	RetrieveBarErrorCodeUnitInvalid       RetrieveBarErrorCode = 2
	RetrieveBarErrorCodeUnitNumberInvalid RetrieveBarErrorCode = 3
	RetrieveBarErrorCodeLimitInvalid      RetrieveBarErrorCode = 4
)

var retrieveBarErrorCodeNames = []string{"Success", "ContractNotFound", "UnitInvalid", "UnitNumberInvalid", "LimitInvalid"}

// String returns the spec name of the value, or "Unrecognized(<n>)" for a
// value the spec does not list.
func (v RetrieveBarErrorCode) String() string { return enumString(retrieveBarErrorCodeNames, int32(v)) }

// IsKnown reports whether the spec lists the value.
func (v RetrieveBarErrorCode) IsKnown() bool { return enumKnown(retrieveBarErrorCodeNames, int32(v)) }

// SearchOrderErrorCode is the errorCode of Order_SearchOrders,
// Order_SearchOpenOrders and Order_SearchOrdersQuery responses (spec schema
// SearchOrderErrorCode). Zero means success.
type SearchOrderErrorCode int32

// SearchOrderErrorCode values.
const (
	SearchOrderErrorCodeSuccess         SearchOrderErrorCode = 0
	SearchOrderErrorCodeAccountNotFound SearchOrderErrorCode = 1
)

var searchOrderErrorCodeNames = []string{"Success", "AccountNotFound"}

// String returns the spec name of the value, or "Unrecognized(<n>)" for a
// value the spec does not list.
func (v SearchOrderErrorCode) String() string { return enumString(searchOrderErrorCodeNames, int32(v)) }

// IsKnown reports whether the spec lists the value.
func (v SearchOrderErrorCode) IsKnown() bool { return enumKnown(searchOrderErrorCodeNames, int32(v)) }

// SearchOrderByIDErrorCode is the errorCode of Order_SearchOrderById responses
// (spec schema SearchOrderByIdErrorCode). Zero means success.
type SearchOrderByIDErrorCode int32

// SearchOrderByIDErrorCode values.
const (
	SearchOrderByIDErrorCodeSuccess       SearchOrderByIDErrorCode = 0
	SearchOrderByIDErrorCodeOrderNotFound SearchOrderByIDErrorCode = 1
)

var searchOrderByIDErrorCodeNames = []string{"Success", "OrderNotFound"}

// String returns the spec name of the value, or "Unrecognized(<n>)" for a
// value the spec does not list.
func (v SearchOrderByIDErrorCode) String() string {
	return enumString(searchOrderByIDErrorCodeNames, int32(v))
}

// IsKnown reports whether the spec lists the value.
func (v SearchOrderByIDErrorCode) IsKnown() bool {
	return enumKnown(searchOrderByIDErrorCodeNames, int32(v))
}

// PlaceOrderErrorCode is the errorCode of Order_PlaceOrder responses (spec
// schema PlaceOrderErrorCode). Zero means success.
type PlaceOrderErrorCode int32

// PlaceOrderErrorCode values.
const (
	PlaceOrderErrorCodeSuccess             PlaceOrderErrorCode = 0
	PlaceOrderErrorCodeAccountNotFound     PlaceOrderErrorCode = 1
	PlaceOrderErrorCodeOrderRejected       PlaceOrderErrorCode = 2
	PlaceOrderErrorCodeInsufficientFunds   PlaceOrderErrorCode = 3
	PlaceOrderErrorCodeAccountViolation    PlaceOrderErrorCode = 4
	PlaceOrderErrorCodeOutsideTradingHours PlaceOrderErrorCode = 5
	PlaceOrderErrorCodeOrderPending        PlaceOrderErrorCode = 6
	PlaceOrderErrorCodeUnknownError        PlaceOrderErrorCode = 7
	PlaceOrderErrorCodeContractNotFound    PlaceOrderErrorCode = 8
	PlaceOrderErrorCodeContractNotActive   PlaceOrderErrorCode = 9
	PlaceOrderErrorCodeAccountRejected     PlaceOrderErrorCode = 10
)

var placeOrderErrorCodeNames = []string{"Success", "AccountNotFound", "OrderRejected", "InsufficientFunds", "AccountViolation", "OutsideTradingHours", "OrderPending", "UnknownError", "ContractNotFound", "ContractNotActive", "AccountRejected"}

// String returns the spec name of the value, or "Unrecognized(<n>)" for a
// value the spec does not list.
func (v PlaceOrderErrorCode) String() string { return enumString(placeOrderErrorCodeNames, int32(v)) }

// IsKnown reports whether the spec lists the value.
func (v PlaceOrderErrorCode) IsKnown() bool { return enumKnown(placeOrderErrorCodeNames, int32(v)) }

// CancelOrderErrorCode is the errorCode of Order_CancelOrder responses (spec
// schema CancelOrderErrorCode). Zero means success.
type CancelOrderErrorCode int32

// CancelOrderErrorCode values.
const (
	CancelOrderErrorCodeSuccess         CancelOrderErrorCode = 0
	CancelOrderErrorCodeAccountNotFound CancelOrderErrorCode = 1
	CancelOrderErrorCodeOrderNotFound   CancelOrderErrorCode = 2
	CancelOrderErrorCodeRejected        CancelOrderErrorCode = 3
	CancelOrderErrorCodePending         CancelOrderErrorCode = 4
	CancelOrderErrorCodeUnknownError    CancelOrderErrorCode = 5
	CancelOrderErrorCodeAccountRejected CancelOrderErrorCode = 6
)

var cancelOrderErrorCodeNames = []string{"Success", "AccountNotFound", "OrderNotFound", "Rejected", "Pending", "UnknownError", "AccountRejected"}

// String returns the spec name of the value, or "Unrecognized(<n>)" for a
// value the spec does not list.
func (v CancelOrderErrorCode) String() string { return enumString(cancelOrderErrorCodeNames, int32(v)) }

// IsKnown reports whether the spec lists the value.
func (v CancelOrderErrorCode) IsKnown() bool { return enumKnown(cancelOrderErrorCodeNames, int32(v)) }

// ModifyOrderErrorCode is the errorCode of Order_ModifyOrder responses (spec
// schema ModifyOrderErrorCode). Zero means success.
type ModifyOrderErrorCode int32

// ModifyOrderErrorCode values.
const (
	ModifyOrderErrorCodeSuccess          ModifyOrderErrorCode = 0
	ModifyOrderErrorCodeAccountNotFound  ModifyOrderErrorCode = 1
	ModifyOrderErrorCodeOrderNotFound    ModifyOrderErrorCode = 2
	ModifyOrderErrorCodeRejected         ModifyOrderErrorCode = 3
	ModifyOrderErrorCodePending          ModifyOrderErrorCode = 4
	ModifyOrderErrorCodeUnknownError     ModifyOrderErrorCode = 5
	ModifyOrderErrorCodeAccountRejected  ModifyOrderErrorCode = 6
	ModifyOrderErrorCodeContractNotFound ModifyOrderErrorCode = 7
)

var modifyOrderErrorCodeNames = []string{"Success", "AccountNotFound", "OrderNotFound", "Rejected", "Pending", "UnknownError", "AccountRejected", "ContractNotFound"}

// String returns the spec name of the value, or "Unrecognized(<n>)" for a
// value the spec does not list.
func (v ModifyOrderErrorCode) String() string { return enumString(modifyOrderErrorCodeNames, int32(v)) }

// IsKnown reports whether the spec lists the value.
func (v ModifyOrderErrorCode) IsKnown() bool { return enumKnown(modifyOrderErrorCodeNames, int32(v)) }

// SearchPositionErrorCode is the errorCode of Position_SearchOpenPositions
// responses (spec schema SearchPositionErrorCode). Zero means success.
type SearchPositionErrorCode int32

// SearchPositionErrorCode values.
const (
	SearchPositionErrorCodeSuccess         SearchPositionErrorCode = 0
	SearchPositionErrorCodeAccountNotFound SearchPositionErrorCode = 1
)

var searchPositionErrorCodeNames = []string{"Success", "AccountNotFound"}

// String returns the spec name of the value, or "Unrecognized(<n>)" for a
// value the spec does not list.
func (v SearchPositionErrorCode) String() string {
	return enumString(searchPositionErrorCodeNames, int32(v))
}

// IsKnown reports whether the spec lists the value.
func (v SearchPositionErrorCode) IsKnown() bool {
	return enumKnown(searchPositionErrorCodeNames, int32(v))
}

// ClosePositionErrorCode is the errorCode of Position_CloseContractPosition
// responses (spec schema ClosePositionErrorCode). Zero means success.
type ClosePositionErrorCode int32

// ClosePositionErrorCode values.
const (
	ClosePositionErrorCodeSuccess           ClosePositionErrorCode = 0
	ClosePositionErrorCodeAccountNotFound   ClosePositionErrorCode = 1
	ClosePositionErrorCodePositionNotFound  ClosePositionErrorCode = 2
	ClosePositionErrorCodeContractNotFound  ClosePositionErrorCode = 3
	ClosePositionErrorCodeContractNotActive ClosePositionErrorCode = 4
	ClosePositionErrorCodeOrderRejected     ClosePositionErrorCode = 5
	ClosePositionErrorCodeOrderPending      ClosePositionErrorCode = 6
	ClosePositionErrorCodeUnknownError      ClosePositionErrorCode = 7
	ClosePositionErrorCodeAccountRejected   ClosePositionErrorCode = 8
)

var closePositionErrorCodeNames = []string{"Success", "AccountNotFound", "PositionNotFound", "ContractNotFound", "ContractNotActive", "OrderRejected", "OrderPending", "UnknownError", "AccountRejected"}

// String returns the spec name of the value, or "Unrecognized(<n>)" for a
// value the spec does not list.
func (v ClosePositionErrorCode) String() string {
	return enumString(closePositionErrorCodeNames, int32(v))
}

// IsKnown reports whether the spec lists the value.
func (v ClosePositionErrorCode) IsKnown() bool {
	return enumKnown(closePositionErrorCodeNames, int32(v))
}

// PartialClosePositionErrorCode is the errorCode of
// Position_PartialCloseContractPosition responses (spec schema
// PartialClosePositionErrorCode). Zero means success.
type PartialClosePositionErrorCode int32

// PartialClosePositionErrorCode values.
const (
	PartialClosePositionErrorCodeSuccess           PartialClosePositionErrorCode = 0
	PartialClosePositionErrorCodeAccountNotFound   PartialClosePositionErrorCode = 1
	PartialClosePositionErrorCodePositionNotFound  PartialClosePositionErrorCode = 2
	PartialClosePositionErrorCodeContractNotFound  PartialClosePositionErrorCode = 3
	PartialClosePositionErrorCodeContractNotActive PartialClosePositionErrorCode = 4
	PartialClosePositionErrorCodeInvalidCloseSize  PartialClosePositionErrorCode = 5
	PartialClosePositionErrorCodeOrderRejected     PartialClosePositionErrorCode = 6
	PartialClosePositionErrorCodeOrderPending      PartialClosePositionErrorCode = 7
	PartialClosePositionErrorCodeUnknownError      PartialClosePositionErrorCode = 8
	PartialClosePositionErrorCodeAccountRejected   PartialClosePositionErrorCode = 9
)

var partialClosePositionErrorCodeNames = []string{"Success", "AccountNotFound", "PositionNotFound", "ContractNotFound", "ContractNotActive", "InvalidCloseSize", "OrderRejected", "OrderPending", "UnknownError", "AccountRejected"}

// String returns the spec name of the value, or "Unrecognized(<n>)" for a
// value the spec does not list.
func (v PartialClosePositionErrorCode) String() string {
	return enumString(partialClosePositionErrorCodeNames, int32(v))
}

// IsKnown reports whether the spec lists the value.
func (v PartialClosePositionErrorCode) IsKnown() bool {
	return enumKnown(partialClosePositionErrorCodeNames, int32(v))
}

// SearchTradeErrorCode is the errorCode of Trade_SearchHalfTurnTrades responses
// (spec schema SearchTradeErrorCode). Zero means success.
type SearchTradeErrorCode int32

// SearchTradeErrorCode values.
const (
	SearchTradeErrorCodeSuccess         SearchTradeErrorCode = 0
	SearchTradeErrorCodeAccountNotFound SearchTradeErrorCode = 1
)

var searchTradeErrorCodeNames = []string{"Success", "AccountNotFound"}

// String returns the spec name of the value, or "Unrecognized(<n>)" for a
// value the spec does not list.
func (v SearchTradeErrorCode) String() string { return enumString(searchTradeErrorCodeNames, int32(v)) }

// IsKnown reports whether the spec lists the value.
func (v SearchTradeErrorCode) IsKnown() bool { return enumKnown(searchTradeErrorCodeNames, int32(v)) }
