package topstepx

import "context"

// TradesService searches trades. Obtain it from Client.Trades.
type TradesService struct{ c *Client }

// Search returns the account's trades in the requested range
// (Trade_SearchHalfTurnTrades). A nil ProfitAndLoss marks a half-turn trade.
func (s *TradesService) Search(ctx context.Context, req SearchTradeRequest) ([]HalfTrade, error) {
	resp, err := call[SearchHalfTradeResponse](ctx, s.c, OpTradeSearchHalfTurnTrades, req)
	if err != nil {
		return nil, err
	}
	return resp.Trades, nil
}
