package topstepx

import "fmt"

// validator is implemented by request types with client-side rules
// (contract section 9). The client runs Validate before any network I/O
// unless WithoutValidation is set.
type validator interface {
	Validate() error
}

// placeable reports whether t may be used for order placement and brackets:
// Limit, Market, Stop, TrailingStop, JoinBid or JoinAsk.
func placeable(t OrderType) bool {
	switch t {
	case OrderTypeLimit, OrderTypeMarket, OrderTypeStop, OrderTypeTrailingStop, OrderTypeJoinBid, OrderTypeJoinAsk:
		return true
	}
	return false
}

func unsupportedType(field string, t OrderType) *ValidationError {
	return &ValidationError{
		Field:  field,
		Reason: fmt.Sprintf("order type %s (%d) is not supported; use Limit, Market, Stop, TrailingStop, JoinBid or JoinAsk", t, int32(t)),
	}
}

// Validate checks the placement rules of the spec: the order type and both
// bracket types must be Limit, Market, Stop, TrailingStop, JoinBid or JoinAsk
// (StopLimit and Unknown are rejected), and a TrailingStop order needs a
// non-nil TrailPrice. It returns a *ValidationError.
func (r PlaceOrderRequest) Validate() error {
	if !placeable(r.Type) {
		return unsupportedType("type", r.Type)
	}
	if r.Type == OrderTypeTrailingStop && r.TrailPrice == nil {
		return &ValidationError{Field: "trailPrice", Reason: "a TrailingStop order requires trailPrice, an absolute stop price"}
	}
	if r.StopLossBracket != nil && !placeable(r.StopLossBracket.Type) {
		return unsupportedType("stopLossBracket.type", r.StopLossBracket.Type)
	}
	if r.TakeProfitBracket != nil && !placeable(r.TakeProfitBracket.Type) {
		return unsupportedType("takeProfitBracket.type", r.TakeProfitBracket.Type)
	}
	return nil
}

// Validate checks that Limit does not exceed MaxBars. It returns a
// *ValidationError.
func (r RetrieveBarRequest) Validate() error {
	if r.Limit > MaxBars {
		return &ValidationError{Field: "limit", Reason: fmt.Sprintf("at most %d bars may be requested, got %d", MaxBars, r.Limit)}
	}
	return nil
}

// Validate checks that Size is at least 1. It returns a *ValidationError.
func (r PartialCloseContractPositionRequest) Validate() error {
	if r.Size < 1 {
		return &ValidationError{Field: "size", Reason: fmt.Sprintf("size must be at least 1, got %d", r.Size)}
	}
	return nil
}
