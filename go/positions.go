package topstepx

import "context"

// PositionsService searches and closes positions. Obtain it from
// Client.Positions.
type PositionsService struct{ c *Client }

// SearchOpen returns the account's open positions
// (Position_SearchOpenPositions).
func (s *PositionsService) SearchOpen(ctx context.Context, accountID int32) ([]Position, error) {
	resp, err := call[SearchPositionResponse](ctx, s.c, OpPositionSearchOpenPositions, SearchPositionRequest{AccountID: accountID})
	if err != nil {
		return nil, err
	}
	return resp.Positions, nil
}

// Close closes the account's position in the contract
// (Position_CloseContractPosition). It is never retried on HTTP 5xx or
// transport errors.
func (s *PositionsService) Close(ctx context.Context, accountID int32, contractID string) error {
	_, err := call[ClosePositionResponse](ctx, s.c, OpPositionCloseContractPosition,
		CloseContractPositionRequest{AccountID: accountID, ContractID: contractID})
	return err
}

// PartialClose reduces the account's position in the contract by size, which
// must be at least 1 (Position_PartialCloseContractPosition). It is never
// retried on HTTP 5xx or transport errors.
func (s *PositionsService) PartialClose(ctx context.Context, accountID int32, contractID string, size int32) error {
	_, err := call[PartialClosePositionResponse](ctx, s.c, OpPositionPartialCloseContractPosition,
		PartialCloseContractPositionRequest{AccountID: accountID, ContractID: contractID, Size: size})
	return err
}
