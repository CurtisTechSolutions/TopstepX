"""Credentials, session state and the token decision table (``ensure_token``).

Internal module. Nothing here performs I/O.
"""

from __future__ import annotations

import base64
import binascii
import enum
import json
import math
from dataclasses import dataclass, field

TOKEN_FALLBACK_LIFETIME = 24 * 3600.0
"""Assumed lifetime of a token without a decodable JWT ``exp`` claim."""
RENEWAL_COOLDOWN = 60.0
"""After a failed renewal that keeps the current token, skip ``validate`` this long."""


@dataclass(frozen=True, slots=True)
class Credentials:
    """Platform username and API key. The key never appears in ``repr``."""

    username: str
    api_key: str = field(repr=False)

    def __repr__(self) -> str:
        return f"Credentials(username={self.username!r}, api_key=<redacted>)"


def make_credentials(username: str | None, api_key: str | None) -> Credentials | None:
    """Build credentials from optional parts; both or neither must be given."""
    if username is None and api_key is None:
        return None
    if not username or not api_key:
        raise ValueError("username and api_key must be provided together")
    return Credentials(username, api_key)


@dataclass(frozen=True, slots=True)
class Session:
    """An immutable snapshot of the stored token and its expiry.

    Replacing the whole snapshot is atomic, so readers never observe a token
    paired with another token's expiry.

    Attributes:
        token: The bearer token (redacted in ``repr``).
        obtained_at: When the token was stored (epoch seconds).
        expires_at: When the token expires (epoch seconds).
        generation: Increases every time a token is stored; used to detect that
            another caller replaced the token.
        renewal_blocked_until: After a failed renewal that kept this token, no
            ``validate`` is attempted before this time (epoch seconds). A newly
            stored token starts without a cooldown.
    """

    token: str = field(repr=False)
    obtained_at: float
    expires_at: float
    generation: int
    renewal_blocked_until: float | None = None

    def __repr__(self) -> str:
        return (
            f"Session(token=<redacted>, obtained_at={self.obtained_at!r}, "
            f"expires_at={self.expires_at!r}, generation={self.generation!r}, "
            f"renewal_blocked_until={self.renewal_blocked_until!r})"
        )


def token_expiry(token: str) -> float | None:
    """Return the JWT ``exp`` claim (epoch seconds) of ``token``, or ``None``.

    The payload segment is base64url-decoded; the signature is never verified.
    """
    parts = token.split(".")
    if len(parts) < 2 or not parts[1]:
        return None
    segment = parts[1]
    try:
        payload = base64.urlsafe_b64decode(segment + "=" * (-len(segment) % 4))
        claims = json.loads(payload)
    except (binascii.Error, ValueError):
        return None
    if not isinstance(claims, dict):
        return None
    exp = claims.get("exp")
    if isinstance(exp, bool) or not isinstance(exp, (int, float)):
        return None
    value = float(exp)
    return value if math.isfinite(value) else None


def new_session(token: str, now: float, generation: int) -> Session:
    """Create a session for a freshly obtained token."""
    expires_at = token_expiry(token)
    if expires_at is None:
        expires_at = now + TOKEN_FALLBACK_LIFETIME
    return Session(token=token, obtained_at=now, expires_at=expires_at, generation=generation)


class TokenAction(enum.Enum):
    """What ``ensure_token`` must do before an authenticated request."""

    USE = "use"
    """Send the request with the stored token."""
    LOGIN = "login"
    """Log in with the configured credentials."""
    VALIDATE = "validate"
    """Renew the token through ``Auth_Validate``."""
    FAIL = "fail"
    """Raise ``NotAuthenticatedError`` without any network I/O."""


def plan_token(
    session: Session | None, now: float, margin: float, has_credentials: bool
) -> TokenAction:
    """The ``ensure_token`` decision table (``SDK_BEHAVIOR.md`` section 5).

    1. No token: log in when credentials exist, otherwise fail.
    2. Expired (``now >= exp``): log in when credentials exist, otherwise use the
       token anyway and let the server decide (tolerates clock skew).
    3. Inside the refresh margin (``now >= exp - margin``): validate, unless a
       failed renewal put the session in its cooldown (``now <
       renewal_blocked_until``), in which case use the token as-is.
    4. Otherwise use the stored token.
    """
    if session is None:
        return TokenAction.LOGIN if has_credentials else TokenAction.FAIL
    if now >= session.expires_at:
        return TokenAction.LOGIN if has_credentials else TokenAction.USE
    if now >= session.expires_at - margin:
        blocked_until = session.renewal_blocked_until
        if blocked_until is not None and now < blocked_until:
            return TokenAction.USE
        return TokenAction.VALIDATE
    return TokenAction.USE
