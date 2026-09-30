package topstepx

import "time"

// Version is the SDK version, also sent in the default User-Agent.
const Version = "0.1.0"

// Default configuration values from the cross-SDK contract (section 2).
const (
	// DefaultBaseURL is the TopstepX REST gateway.
	DefaultBaseURL = "https://api.topstepx.com"
	// DefaultUserHubURL is the TopstepX SignalR user hub.
	DefaultUserHubURL = "https://rtc.topstepx.com/hubs/user"
	// DefaultMarketHubURL is the TopstepX SignalR market hub.
	DefaultMarketHubURL = "https://rtc.topstepx.com/hubs/market"
	// DefaultUserAgent is sent with every request unless WithUserAgent overrides it.
	DefaultUserAgent = "topstepx-go/" + Version
	// DefaultTimeout bounds each HTTP attempt.
	DefaultTimeout = 30 * time.Second
	// DefaultConnectTimeout bounds TCP connection setup and the TLS handshake
	// of the default HTTP client.
	DefaultConnectTimeout = 10 * time.Second
	// DefaultMaxRetries is the retry budget for throttled and failed attempts.
	DefaultMaxRetries = 3
	// DefaultRetryBackoff is the first retry delay; it doubles per retry.
	DefaultRetryBackoff = 1 * time.Second
	// DefaultMaxRetryBackoff caps the retry delay.
	DefaultMaxRetryBackoff = 30 * time.Second
	// DefaultRefreshMargin is how long before expiry a token is renewed.
	DefaultRefreshMargin = time.Hour
	// DefaultTokenLifetime is assumed for tokens without a decodable JWT exp claim.
	DefaultTokenLifetime = 24 * time.Hour
)
