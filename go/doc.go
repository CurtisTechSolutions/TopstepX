// Package topstepx is a Go client for the ProjectX Gateway API as deployed by
// TopstepX.
//
// It implements the cross-SDK behavior contract in spec/SDK_BEHAVIOR.md: typed
// models for all 21 REST operations, token lifecycle management (lazy API-key
// login, refresh through validate, single-flight re-login on HTTP 401),
// business-failure detection, retries, client-side rate limiting and request
// validation. The realtime SignalR hubs live in the realtime subpackage.
//
// # Quick start
//
//	client, err := topstepx.NewClient(
//		topstepx.WithCredentials(os.Getenv("TOPSTEPX_USERNAME"), os.Getenv("TOPSTEPX_API_KEY")),
//	)
//	if err != nil {
//		log.Fatal(err)
//	}
//	accounts, err := client.Accounts.Search(ctx, true)
//
// The first authenticated call logs in with the API key and stores the token.
// Every method takes a context.Context and the Client is safe for concurrent
// use.
//
// # Errors
//
// A response with success == false or a non-zero errorCode becomes an
// *APIError, even though it arrives with HTTP 200. Other failures are reported
// as *HTTPError (ErrUnauthorized for 401), *RateLimitError, *ValidationError,
// *DecodeError or *TransportError. Use errors.Is and errors.As to inspect them.
//
// # Wire format
//
// Decimal fields use github.com/shopspring/decimal and are always sent as JSON
// numbers. Timestamps are time.Time values in UTC; they are sent as RFC 3339
// with a "Z" suffix and decoded leniently (any offset, a missing offset read as
// UTC, and 0 to 9 fractional digits). Optional and nullable fields are
// pointers; use Ptr to fill them. Arrays missing from a response decode as nil
// slices, which behave as empty slices.
//
// Orders placed through this package are real. Use simulated accounts while
// developing.
package topstepx
