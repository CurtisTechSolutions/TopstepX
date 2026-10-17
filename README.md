# TopstepX

SDKs for the [ProjectX Gateway API](https://gateway.docs.projectx.com/docs/intro/) as deployed
by TopstepX, in [Rust](rust/), [Python](python/) and [Go](go/). All three implement the same
behavior, defined in [`spec/SDK_BEHAVIOR.md`](spec/SDK_BEHAVIOR.md), against the REST contract
in [`spec/`](spec/).

> **Orders are real.** Placing, modifying, cancelling and closing act on real accounts. Develop
> against a simulated account.

## Installation

Each SDK is released on its own (see [`CONTRIBUTING.md`](CONTRIBUTING.md#releases)). Until an
SDK's first release reaches its registry, install it from git as shown below.

### Python

Python 3.10 or later. The distribution is `topstepx`, imported as `import topstepx`.

```sh
pip install topstepx
```

From git:

```sh
pip install "topstepx @ git+https://github.com/CurtisTechSolutions/TopstepX#subdirectory=python"
```

### Rust

Rust 1.88 or later.

```sh
cargo add topstepx
cargo add tokio --features macros,rt-multi-thread
```

From git (cargo finds the package in the `rust/` directory):

```toml
[dependencies]
topstepx = { git = "https://github.com/CurtisTechSolutions/TopstepX" }
tokio = { version = "1", features = ["macros", "rt-multi-thread"] }
```

The `rustls` and `realtime` features are on by default; see the
[Rust README](rust/README.md#installation) for `native-tls`.

### Go

Go 1.22 or later.

```sh
go get github.com/CurtisTechSolutions/TopstepX/go
```

```go
import (
	topstepx "github.com/CurtisTechSolutions/TopstepX/go"
	"github.com/CurtisTechSolutions/TopstepX/go/realtime"
)
```

### Credentials

The examples log in with your platform username and an API key, read from the
`TOPSTEPX_USERNAME` and `TOPSTEPX_API_KEY` environment variables:

```sh
export TOPSTEPX_USERNAME=your-username
export TOPSTEPX_API_KEY=your-api-key
```

## Usage

Each SDK's README covers configuration, authentication, the endpoint groups, the realtime hubs
and error handling:

- [Rust](rust/README.md)
- [Python](python/README.md)
- [Go](go/README.md)

## License

[GPL-3.0-only](LICENSE)
