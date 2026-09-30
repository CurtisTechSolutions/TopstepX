package topstepx

import "context"

// AccountsService searches trading accounts. Obtain it from Client.Accounts.
type AccountsService struct{ c *Client }

// Search returns the trading accounts (Account_SearchAccounts). With
// onlyActiveAccounts set, only active accounts are returned.
func (s *AccountsService) Search(ctx context.Context, onlyActiveAccounts bool) ([]TradingAccount, error) {
	resp, err := call[SearchAccountResponse](ctx, s.c, OpAccountSearchAccounts, SearchAccountRequest{OnlyActiveAccounts: onlyActiveAccounts})
	if err != nil {
		return nil, err
	}
	return resp.Accounts, nil
}
