package topstepx

import (
	"context"
	"errors"
)

// ContractsService searches contracts. Obtain it from Client.Contracts.
//
// The live flag of each method selects the live or simulated market-data
// subscription; it says nothing about whether an account is simulated.
type ContractsService struct{ c *Client }

// Search returns up to 20 contracts whose names match searchText
// (Contract_SearchContracts).
func (s *ContractsService) Search(ctx context.Context, searchText string, live bool) ([]Contract, error) {
	resp, err := call[SearchContractResponse](ctx, s.c, OpContractSearchContracts, SearchContractRequest{SearchText: searchText, Live: live})
	if err != nil {
		return nil, err
	}
	return resp.Contracts, nil
}

// SearchByID returns the contract with the given identifier
// (Contract_SearchContractById). A successful response without a contract is
// reported as a *DecodeError.
func (s *ContractsService) SearchByID(ctx context.Context, contractID string) (*Contract, error) {
	resp, err := call[SearchContractByIDResponse](ctx, s.c, OpContractSearchContractByID, SearchContractByIDRequest{ContractID: contractID})
	if err != nil {
		return nil, err
	}
	if resp.Contract == nil {
		return nil, &DecodeError{Operation: OpContractSearchContractByID, StatusCode: 200, Err: errors.New("successful response has no contract")}
	}
	return resp.Contract, nil
}

// Available returns the contracts available for the selected data
// subscription (Contract_AvailableContracts).
func (s *ContractsService) Available(ctx context.Context, live bool) ([]Contract, error) {
	resp, err := call[ListAvailableContractResponse](ctx, s.c, OpContractAvailableContracts, ListAvailableContractRequest{Live: live})
	if err != nil {
		return nil, err
	}
	return resp.Contracts, nil
}
