package topstepx

import (
	"context"
	"errors"
)

// OrdersService searches and manages orders. Obtain it from Client.Orders.
//
// Orders are real. Use simulated accounts while developing.
type OrdersService struct{ c *Client }

// Search returns the account's orders created in the requested range
// (Order_SearchOrders).
func (s *OrdersService) Search(ctx context.Context, req SearchOrderRequest) ([]Order, error) {
	resp, err := call[SearchOrderResponse](ctx, s.c, OpOrderSearchOrders, req)
	if err != nil {
		return nil, err
	}
	return resp.Orders, nil
}

// SearchByID returns one order of the account (Order_SearchOrderById, listed
// only in Swagger). A successful response without an order is reported as a
// *DecodeError.
func (s *OrdersService) SearchByID(ctx context.Context, accountID int32, orderID int64) (*Order, error) {
	resp, err := call[SearchOrderByIDResponse](ctx, s.c, OpOrderSearchOrderByID, SearchOrderByIDRequest{AccountID: accountID, OrderID: orderID})
	if err != nil {
		return nil, err
	}
	if resp.Order == nil {
		return nil, &DecodeError{Operation: OpOrderSearchOrderByID, StatusCode: 200, Err: errors.New("successful response has no order")}
	}
	return resp.Order, nil
}

// SearchOpen returns the account's open orders (Order_SearchOpenOrders).
func (s *OrdersService) SearchOpen(ctx context.Context, accountID int32) ([]Order, error) {
	resp, err := call[SearchOrderResponse](ctx, s.c, OpOrderSearchOpenOrders, SearchOpenOrderRequest{AccountID: accountID})
	if err != nil {
		return nil, err
	}
	return resp.Orders, nil
}

// OrdersQueryResult is the payload of Orders.Query.
type OrdersQueryResult struct {
	// Orders are the orders of the requested page.
	Orders []Order
	// TotalCount is the number of matching orders, present when requested
	// with IncludeTotalCount.
	TotalCount *int32
}

// Query filters, sorts and pages the account's orders
// (Order_SearchOrdersQuery, listed only in Swagger). An HTTP 400 response
// carries a parsed ProblemDetails in the returned *HTTPError.
func (s *OrdersService) Query(ctx context.Context, req SearchOrdersQueryRequest) (*OrdersQueryResult, error) {
	resp, err := call[SearchOrdersQueryResponse](ctx, s.c, OpOrderSearchOrdersQuery, req)
	if err != nil {
		return nil, err
	}
	return &OrdersQueryResult{Orders: resp.Orders, TotalCount: resp.TotalCount}, nil
}

// Place submits an order (Order_PlaceOrder) and returns its identifier.
//
// A rejected order is an *APIError whose OrderID holds the identifier the
// gateway may still return; that identifier does not mean the order was
// accepted. Place is never retried on HTTP 5xx or transport errors, since the
// order may already exist. Brackets need the account's Auto OCO Brackets
// mode, and PlaceOrderRequest.TrailPrice is an absolute price.
func (s *OrdersService) Place(ctx context.Context, req PlaceOrderRequest) (int64, error) {
	resp, err := call[PlaceOrderResponse](ctx, s.c, OpOrderPlaceOrder, req)
	if err != nil {
		return 0, err
	}
	if resp.OrderID == nil {
		return 0, &DecodeError{Operation: OpOrderPlaceOrder, StatusCode: 200, Err: errors.New("successful response has no orderId")}
	}
	return *resp.OrderID, nil
}

// Cancel cancels an order (Order_CancelOrder). Cancellation is documented
// for simulated accounts that are not copy-trading followers.
func (s *OrdersService) Cancel(ctx context.Context, accountID int32, orderID int64) error {
	_, err := call[CancelOrderResponse](ctx, s.c, OpOrderCancelOrder, CancelOrderRequest{AccountID: accountID, OrderID: orderID})
	return err
}

// Modify changes an open order (Order_ModifyOrder). Nil fields of req are
// left unchanged; ModifyOrderRequest.TrailPrice is an absolute level.
func (s *OrdersService) Modify(ctx context.Context, req ModifyOrderRequest) error {
	_, err := call[ModifyOrderResponse](ctx, s.c, OpOrderModifyOrder, req)
	return err
}
