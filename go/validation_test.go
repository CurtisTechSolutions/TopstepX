package topstepx

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/shopspring/decimal"
)

// negativeCases reproduces every negative case of spec/validation-report.json
// (contract section 9). Each returns the request that must be rejected and the
// offending field.
var negativeCases = map[string]func() (validator, string){
	"trailing-stop requires trailPrice": func() (validator, string) {
		r := PlaceOrderRequest{AccountID: 123, ContractID: "CON.F.US.EP.U25", Type: OrderTypeTrailingStop, Side: OrderSideAsk, Size: 1}
		return r, "trailPrice"
	},
	"trailing-stop rejects null trailPrice": func() (validator, string) {
		var r PlaceOrderRequest
		if err := r.UnmarshalJSON([]byte(`{"accountId":123,"contractId":"CON.F.US.EP.U25","type":5,"side":1,"size":1,"trailPrice":null}`)); err != nil {
			panic(err)
		}
		return r, "trailPrice"
	},
	"placement rejects unsupported StopLimit": func() (validator, string) {
		return NewMarketOrder(123, "CON.F.US.EP.U25", OrderSideBid, 1).withType(OrderTypeStopLimit), "type"
	},
	"bar count cannot exceed 20000": func() (validator, string) {
		return RetrieveBarRequest{ContractID: "CON.F.US.EP.U25", Unit: AggregateBarUnitMinute, UnitNumber: 1, Limit: 20001}, "limit"
	},
	"partial close rejects zero": func() (validator, string) {
		return PartialCloseContractPositionRequest{AccountID: 123, ContractID: "CON.F.US.EP.U25", Size: 0}, "size"
	},
}

func (r PlaceOrderRequest) withType(t OrderType) PlaceOrderRequest {
	r.Type = t
	return r
}

func TestValidationReportNegativeCases(t *testing.T) {
	report := readValidationReport(t)
	if len(report.NegativeCases) != len(negativeCases) {
		t.Errorf("report lists %d negative cases, tests cover %d", len(report.NegativeCases), len(negativeCases))
	}
	for _, nc := range report.NegativeCases {
		build, ok := negativeCases[nc.Name]
		if !ok {
			t.Errorf("negative case %q is not reproduced", nc.Name)
			continue
		}
		req, field := build()
		err := req.Validate()
		var ve *ValidationError
		if !errors.As(err, &ve) || ve.Field != field {
			t.Errorf("%s: Validate() = %v, want *ValidationError on %q", nc.Name, err, field)
		}
	}
}

func TestValidationMoreNegativeCases(t *testing.T) {
	price := decimal.RequireFromString("6000")
	cases := map[string]struct {
		req   validator
		field string
	}{
		"unknown type":             {NewMarketOrder(1, "C", OrderSideBid, 1).withType(OrderTypeUnknown), "type"},
		"unrecognized type":        {NewMarketOrder(1, "C", OrderSideBid, 1).withType(OrderType(42)), "type"},
		"stop-loss StopLimit":      {NewMarketOrder(1, "C", OrderSideBid, 1).WithStopLossBracket(8, OrderTypeStopLimit), "stopLossBracket.type"},
		"take-profit Unknown":      {NewLimitOrder(1, "C", OrderSideBid, 1, price).WithTakeProfitBracket(16, OrderTypeUnknown), "takeProfitBracket.type"},
		"take-profit unrecognized": {NewMarketOrder(1, "C", OrderSideBid, 1).WithTakeProfitBracket(16, OrderType(-1)), "takeProfitBracket.type"},
		"partial close negative":   {PartialCloseContractPositionRequest{AccountID: 1, ContractID: "C", Size: -3}, "size"},
	}
	for name, c := range cases {
		var ve *ValidationError
		if err := c.req.Validate(); !errors.As(err, &ve) || ve.Field != c.field {
			t.Errorf("%s: Validate() = %v, want error on %q", name, err, c.field)
		}
	}
}

func TestValidationPositiveCases(t *testing.T) {
	price := decimal.RequireFromString("5998.5")
	cases := map[string]validator{
		"market":                NewMarketOrder(1, "C", OrderSideBid, 1),
		"limit":                 NewLimitOrder(1, "C", OrderSideBid, 1, price),
		"stop":                  NewStopOrder(1, "C", OrderSideAsk, 1, price),
		"trailing stop":         NewTrailingStopOrder(1, "C", OrderSideAsk, 1, price),
		"join bid":              NewJoinBidOrder(1, "C", OrderSideBid, 1),
		"join ask":              NewJoinAskOrder(1, "C", OrderSideAsk, 1),
		"brackets":              NewMarketOrder(1, "C", OrderSideBid, 1).WithStopLossBracket(8, OrderTypeStop).WithTakeProfitBracket(16, OrderTypeLimit),
		"bars at the limit":     RetrieveBarRequest{Limit: MaxBars},
		"bars zero":             RetrieveBarRequest{Limit: 0},
		"partial close one":     PartialCloseContractPositionRequest{AccountID: 1, ContractID: "C", Size: 1},
		"trailing via pointers": PlaceOrderRequest{Type: OrderTypeTrailingStop, TrailPrice: Ptr(price)},
	}
	for name, req := range cases {
		if err := req.Validate(); err != nil {
			t.Errorf("%s: Validate() = %v, want nil", name, err)
		}
	}
}

// Validation runs before any network I/O, login included, and can be
// disabled per client.
func TestValidationRunsBeforeNetworkIO(t *testing.T) {
	m := newMockServer(t)
	m.handleLogin("token")
	m.handle("/api/Order/place", okJSON(`{"success":true,"errorCode":0,"orderId":1}`))
	m.handle("/api/Position/partialCloseContract", okJSON(`{"success":true,"errorCode":0}`))
	m.handle("/api/History/retrieveBars", okJSON(`{"success":true,"errorCode":0,"bars":[]}`))
	c := m.client(t, WithCredentials("user", "key"))
	ctx := context.Background()

	_, err := c.Orders.Place(ctx, NewMarketOrder(1, "C", OrderSideBid, 1).withType(OrderTypeStopLimit))
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Operation != OpOrderPlaceOrder || ve.Field != "type" {
		t.Fatalf("Place: %v, want *ValidationError", err)
	}
	if err := c.Positions.PartialClose(ctx, 1, "C", 0); !errors.As(err, &ve) {
		t.Fatalf("PartialClose: %v, want *ValidationError", err)
	}
	if _, err := c.History.RetrieveBars(ctx, RetrieveBarRequest{Limit: 20001}); !errors.As(err, &ve) {
		t.Fatalf("RetrieveBars: %v, want *ValidationError", err)
	}
	if err := c.Do(ctx, OpOrderPlaceOrder, NewTrailingStopOrder(1, "C", OrderSideAsk, 1, decimal.Zero).withTrail(nil), nil); !errors.As(err, &ve) {
		t.Fatalf("Do: %v, want *ValidationError", err)
	}
	if n := m.total(); n != 0 {
		t.Fatalf("server saw %d requests; validation must precede login and I/O", n)
	}

	// Disabled validation lets the server decide.
	c = m.client(t, WithCredentials("user", "key"), WithoutValidation())
	if _, err := c.Orders.Place(ctx, NewMarketOrder(1, "C", OrderSideBid, 1).withType(OrderTypeStopLimit)); err != nil {
		t.Fatalf("Place without validation: %v", err)
	}
	if got := m.count("/api/Order/place"); got != 1 {
		t.Fatalf("place calls = %d, want 1", got)
	}
	if got := m.last("/api/Order/place"); got.Method != http.MethodPost {
		t.Fatalf("unexpected request %+v", got)
	}
}

func (r PlaceOrderRequest) withTrail(p *decimal.Decimal) PlaceOrderRequest {
	r.TrailPrice = p
	return r
}
