package topstepx

import "context"

// HistoryService retrieves historical bars. Obtain it from Client.History.
type HistoryService struct{ c *Client }

// RetrieveBars returns historical OHLCV bars (History_GetBars). At most
// MaxBars bars may be requested. The operation has its own rate limit of 50
// requests per 30 seconds.
func (s *HistoryService) RetrieveBars(ctx context.Context, req RetrieveBarRequest) ([]AggregateBar, error) {
	resp, err := call[RetrieveBarResponse](ctx, s.c, OpHistoryGetBars, req)
	if err != nil {
		return nil, err
	}
	return resp.Bars, nil
}
