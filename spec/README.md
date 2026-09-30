# ProjectX Gateway OpenAPI reference

Built on **September 29, 2026** from the [ProjectX documentation](https://gateway.docs.projectx.com/docs/intro/) and its linked [Swagger schema](https://api.topstepx.com/swagger/v1/swagger.json).

## Files

- **projectx-openapi.yaml** — OpenAPI 3.1.0 specification.
- **projectx-openapi.json** — the same specification in JSON.
- **realtime.md** — SignalR connection and subscription reference.
- **validation-report.json** — results and limits of the checks performed.

Import either specification file into a tool that supports OpenAPI 3.1. Both describe the same 21 REST operations and 69 component schemas. Specification version `2026-09-29.1` identifies this derived document, not a vendor API release.

The five operations present only in Swagger are marked with `x-documentation-status: swagger-only`. In particular, **application login is not supported by the current authentication guide**; use API-key login. Inclusion in this inventory does not verify runtime availability.

## Connection and authentication

The documented server is `https://api.topstepx.com`. It is the TopstepX connection; confirm the host if integrating with another firm. [Connection source](https://gateway.docs.projectx.com/docs/getting-started/connection-urls/)

Call `POST /api/Auth/loginKey` using your platform username and API key, then send `Authorization: Bearer <token>`. Check the response body even after HTTP 200. Refresh before the 24-hour expiry; if already expired, log in again. Credentials and tokens in examples are placeholders. [Authentication source](https://gateway.docs.projectx.com/docs/getting-started/authenticate/authenticate-api-key/)

Examples are synthetic, use historical contract identifiers, and were not executed. Replace identifiers with values returned by account and contract discovery. The `live` flag selects a market-data subscription; do not infer account simulation status from it.

## Coverage

| Method | Path | Source |
| --- | --- | --- |
| POST | `/api/Account/search` | [Guide](https://gateway.docs.projectx.com/docs/api-reference/account/search-accounts/) |
| POST | `/api/Auth/loginApp` | [Swagger only](https://api.topstepx.com/swagger/v1/swagger.json) |
| POST | `/api/Auth/loginKey` | [Guide](https://gateway.docs.projectx.com/docs/getting-started/authenticate/authenticate-api-key/) |
| POST | `/api/Auth/logout` | [Swagger only](https://api.topstepx.com/swagger/v1/swagger.json) |
| POST | `/api/Auth/validate` | [Guide](https://gateway.docs.projectx.com/docs/getting-started/validate-session/) |
| POST | `/api/Contract/search` | [Guide](https://gateway.docs.projectx.com/docs/api-reference/market-data/search-contracts/) |
| POST | `/api/Contract/searchById` | [Guide](https://gateway.docs.projectx.com/docs/api-reference/market-data/search-contracts-by-id/) |
| POST | `/api/Contract/available` | [Guide](https://gateway.docs.projectx.com/docs/api-reference/market-data/available-contracts/) |
| POST | `/api/History/retrieveBars` | [Guide](https://gateway.docs.projectx.com/docs/api-reference/market-data/retrieve-bars/) |
| POST | `/api/Order/search` | [Guide](https://gateway.docs.projectx.com/docs/api-reference/order/order-search/) |
| POST | `/api/Order/searchById` | [Swagger only](https://api.topstepx.com/swagger/v1/swagger.json) |
| POST | `/api/Order/searchOpen` | [Guide](https://gateway.docs.projectx.com/docs/api-reference/order/order-search-open/) |
| POST | `/api/Order/v2/query` | [Swagger only](https://api.topstepx.com/swagger/v1/swagger.json) |
| POST | `/api/Order/place` | [Guide](https://gateway.docs.projectx.com/docs/api-reference/order/order-place/) |
| POST | `/api/Order/cancel` | [Guide](https://gateway.docs.projectx.com/docs/api-reference/order/order-cancel/) |
| POST | `/api/Order/modify` | [Guide](https://gateway.docs.projectx.com/docs/api-reference/order/order-modify/) |
| POST | `/api/Position/searchOpen` | [Guide](https://gateway.docs.projectx.com/docs/api-reference/positions/search-open-positions/) |
| POST | `/api/Position/closeContract` | [Guide](https://gateway.docs.projectx.com/docs/api-reference/positions/close-positions/) |
| POST | `/api/Position/partialCloseContract` | [Guide](https://gateway.docs.projectx.com/docs/api-reference/positions/close-positions-partial/) |
| GET | `/api/Status/ping` | [Swagger only](https://api.topstepx.com/swagger/v1/swagger.json) |
| POST | `/api/Trade/search` | [Guide](https://gateway.docs.projectx.com/docs/api-reference/trade/trade-search/) |

## Source conflicts and modeling decisions

1. **Missing security declarations.** Swagger does not define authentication. The bearer scheme comes from the authentication guide. Login operations have no bearer requirement. Other operations inherit bearer authentication, including Swagger-only status ping; anonymous ping behavior is unverified.

2. **Required response fields.** Current Swagger requires `hasBusinessFailure`, and account schemas require `simulated`. Some guide examples omit them. This specification preserves the current schema requirements. Its synthetic examples include those fields; they are not captured responses.

3. **Nullability.** Swagger lacks nullable declarations for several fields shown as null in the guides. Explicit null support was added for `errorMessage`, failed-login `token`, optional order prices/tags/brackets, modification values, optional range ends, and trade `profitAndLoss`. Optional fields were not automatically made nullable. The P&L field remains required but accepts null.

4. **Trade search start.** The guide requires `startTimestamp`; Swagger requires only `accountId`. This artifact follows the guide and preserves the original requirement in `x-upstream-required`. [Trade-search source](https://gateway.docs.projectx.com/docs/api-reference/trade/trade-search/)

5. **Contract identifiers.** The bars guide labels `contractId` as integer, while its example and Swagger use string. The specification uses string. The documented 20,000-bar ceiling is encoded. [Bars source](https://gateway.docs.projectx.com/docs/api-reference/market-data/retrieve-bars/)

6. **Enums.** Swagger includes additional aggregation values (Unspecified and Tick), order statuses, and response metadata beyond the guides. These are retained and flagged where relevant. Placement and bracket request types are narrowed to the guide's six supported values; response OrderType retains the complete Swagger enum.

7. **Trailing stops and brackets.** A placement request with type 5 requires non-null `trailPrice`. Request values are absolute prices; returned trailing-stop `trailPrice` is a distance in price units. Bracket objects require Auto OCO Brackets mode. These rules are documented in the operation and schema descriptions. [Placement source](https://gateway.docs.projectx.com/docs/api-reference/order/order-place/)

8. **Unknown details remain unspecified.** Pagination defaults and limits for `/api/Order/v2/query`, unexplained bar fields `d` and `k`, and undocumented error-body shapes are not invented. HTTP 401 and 429 responses therefore have descriptions without body schemas. The upstream decimal format is preserved.

9. **Token-renewal conflict.** The validation page suggests renewal after expiration; the current login guide says renew before expiry or authenticate again. This artifact documents the latter conservative approach. [Validation source](https://gateway.docs.projectx.com/docs/getting-started/validate-session/)

## Limits and failures

Historical bars allow 50 requests per 30 seconds; other endpoints allow 200 per 60 seconds. HTTP 429 indicates throttling. No retry-header or counter-scope guarantees were found. [Rate-limit source](https://gateway.docs.projectx.com/docs/getting-started/rate-limits/)

Business errors use the endpoint-specific numeric enums. An order identifier can be returned even when placement fails, and an error message can be null. Cancellation supports simulated accounts that are not followers. [Placement](https://gateway.docs.projectx.com/docs/api-reference/order/order-place/), [cancellation](https://gateway.docs.projectx.com/docs/api-reference/order/order-cancel/)

## Verification

Passed custom checks for JSON round-trip, 21 operations, 69 schemas, 200 internal references, unique operation IDs, and all 44 request/response examples. Five negative cases cover trailing-stop requirements, unsupported placement type, the bar limit, and positive partial-close quantity.

A full OpenAPI standards validator and independent YAML parser were not available in this chat. YAML was generated from the same checked object as JSON. No authenticated API calls, order submissions, or other trading actions were performed.

Chrome automation could not initialize because the local workspace path was supplied in an incompatible format. Source collection used public web retrieval instead.
