# ProjectX realtime companion

SignalR subscriptions are separate from the REST OpenAPI paths. This reference follows the [official realtime guide](https://gateway.docs.projectx.com/docs/realtime/) and [connection URLs](https://gateway.docs.projectx.com/docs/getting-started/connection-urls/), inspected September 29, 2026.

| Hub | URL |
| --- | --- |
| User | `https://rtc.topstepx.com/hubs/user` |
| Market | `https://rtc.topstepx.com/hubs/market` |

Use the same JWT as REST. The guide demonstrates SignalR with WebSockets, an access-token factory, automatic reconnect, and subscriptions restored after reconnection.

| Subscription method | Argument | Event |
| --- | --- | --- |
| `SubscribeAccounts` | None | `GatewayUserAccount` |
| `SubscribeOrders` | accountId | `GatewayUserOrder` |
| `SubscribePositions` | accountId | `GatewayUserPosition` |
| `SubscribeTrades` | accountId | `GatewayUserTrade` |
| `SubscribeContractQuotes` | contractId | `GatewayQuote` |
| `SubscribeContractTrades` | contractId | `GatewayTrade` |
| `SubscribeContractMarketDepth` | contractId | `GatewayDepth` |

Each method has an `Unsubscribe` counterpart with the same suffix and argument. User-event callbacks receive data; market-event callbacks receive contractId and data.

The guide documents account, order, position, trade, quote, and depth payloads. Follow its event schemas directly: REST models are not asserted to be identical. Quote data includes prices and session statistics; depth data includes price-level volumes; market trades include price, volume, timestamp, and side/type.

This companion is a connection index, not an AsyncAPI specification. See the linked guide for complete event fields and enum definitions. No realtime connection was opened.
