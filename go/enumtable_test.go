package topstepx

// specEnumConstants lists the enum constants by spec schema. It was generated
// from the spec names only: the conformance test checks each constant's value
// and String name against the spec at run time.
var specEnumConstants = map[string][]enumConstant{
	"SearchAccountErrorCode":         {SearchAccountErrorCodeSuccess},
	"LoginErrorCode":                 {LoginErrorCodeSuccess, LoginErrorCodeUserNotFound, LoginErrorCodePasswordVerificationFailed, LoginErrorCodeInvalidCredentials, LoginErrorCodeAppNotFound, LoginErrorCodeAppVerificationFailed, LoginErrorCodeInvalidDevice, LoginErrorCodeAgreementsNotSigned, LoginErrorCodeUnknownError, LoginErrorCodeAPISubscriptionNotFound, LoginErrorCodeAPIKeyAuthenticationDisabled},
	"LogoutErrorCode":                {LogoutErrorCodeSuccess, LogoutErrorCodeInvalidSession, LogoutErrorCodeUnknownError},
	"ValidateErrorCode":              {ValidateErrorCodeSuccess, ValidateErrorCodeInvalidSession, ValidateErrorCodeSessionNotFound, ValidateErrorCodeExpiredToken, ValidateErrorCodeUnknownError},
	"SearchContractErrorCode":        {SearchContractErrorCodeSuccess},
	"SearchContractByIdErrorCode":    {SearchContractByIDErrorCodeSuccess, SearchContractByIDErrorCodeContractNotFound},
	"ListAvailableContractErrorCode": {ListAvailableContractErrorCodeSuccess},
	"RetrieveBarErrorCode":           {RetrieveBarErrorCodeSuccess, RetrieveBarErrorCodeContractNotFound, RetrieveBarErrorCodeUnitInvalid, RetrieveBarErrorCodeUnitNumberInvalid, RetrieveBarErrorCodeLimitInvalid},
	"AggregateBarUnit":               {AggregateBarUnitUnspecified, AggregateBarUnitSecond, AggregateBarUnitMinute, AggregateBarUnitHour, AggregateBarUnitDay, AggregateBarUnitWeek, AggregateBarUnitMonth, AggregateBarUnitTick},
	"SearchOrderErrorCode":           {SearchOrderErrorCodeSuccess, SearchOrderErrorCodeAccountNotFound},
	"OrderStatus":                    {OrderStatusNone, OrderStatusOpen, OrderStatusFilled, OrderStatusCancelled, OrderStatusExpired, OrderStatusRejected, OrderStatusPending, OrderStatusPendingCancellation, OrderStatusSuspended},
	"OrderType":                      {OrderTypeUnknown, OrderTypeLimit, OrderTypeMarket, OrderTypeStopLimit, OrderTypeStop, OrderTypeTrailingStop, OrderTypeJoinBid, OrderTypeJoinAsk},
	"OrderSide":                      {OrderSideBid, OrderSideAsk},
	"SearchOrderByIdErrorCode":       {SearchOrderByIDErrorCodeSuccess, SearchOrderByIDErrorCodeOrderNotFound},
	"OrderSortField":                 {OrderSortFieldCreatedAt, OrderSortFieldID},
	"SortDirection":                  {SortDirectionAsc, SortDirectionDesc},
	"PlaceOrderErrorCode":            {PlaceOrderErrorCodeSuccess, PlaceOrderErrorCodeAccountNotFound, PlaceOrderErrorCodeOrderRejected, PlaceOrderErrorCodeInsufficientFunds, PlaceOrderErrorCodeAccountViolation, PlaceOrderErrorCodeOutsideTradingHours, PlaceOrderErrorCodeOrderPending, PlaceOrderErrorCodeUnknownError, PlaceOrderErrorCodeContractNotFound, PlaceOrderErrorCodeContractNotActive, PlaceOrderErrorCodeAccountRejected},
	"CancelOrderErrorCode":           {CancelOrderErrorCodeSuccess, CancelOrderErrorCodeAccountNotFound, CancelOrderErrorCodeOrderNotFound, CancelOrderErrorCodeRejected, CancelOrderErrorCodePending, CancelOrderErrorCodeUnknownError, CancelOrderErrorCodeAccountRejected},
	"ModifyOrderErrorCode":           {ModifyOrderErrorCodeSuccess, ModifyOrderErrorCodeAccountNotFound, ModifyOrderErrorCodeOrderNotFound, ModifyOrderErrorCodeRejected, ModifyOrderErrorCodePending, ModifyOrderErrorCodeUnknownError, ModifyOrderErrorCodeAccountRejected, ModifyOrderErrorCodeContractNotFound},
	"SearchPositionErrorCode":        {SearchPositionErrorCodeSuccess, SearchPositionErrorCodeAccountNotFound},
	"PositionType":                   {PositionTypeUndefined, PositionTypeLong, PositionTypeShort},
	"ClosePositionErrorCode":         {ClosePositionErrorCodeSuccess, ClosePositionErrorCodeAccountNotFound, ClosePositionErrorCodePositionNotFound, ClosePositionErrorCodeContractNotFound, ClosePositionErrorCodeContractNotActive, ClosePositionErrorCodeOrderRejected, ClosePositionErrorCodeOrderPending, ClosePositionErrorCodeUnknownError, ClosePositionErrorCodeAccountRejected},
	"PartialClosePositionErrorCode":  {PartialClosePositionErrorCodeSuccess, PartialClosePositionErrorCodeAccountNotFound, PartialClosePositionErrorCodePositionNotFound, PartialClosePositionErrorCodeContractNotFound, PartialClosePositionErrorCodeContractNotActive, PartialClosePositionErrorCodeInvalidCloseSize, PartialClosePositionErrorCodeOrderRejected, PartialClosePositionErrorCodeOrderPending, PartialClosePositionErrorCodeUnknownError, PartialClosePositionErrorCodeAccountRejected},
	"SearchTradeErrorCode":           {SearchTradeErrorCodeSuccess, SearchTradeErrorCodeAccountNotFound},
}
