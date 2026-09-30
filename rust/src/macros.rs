//! Declarative macros shared by the model modules.

/// Defines an integer-backed enumeration that never fails to decode.
///
/// Values the specification does not list decode to `Unrecognized(n)`, equality and hashing
/// compare the numeric value, and serialization always writes the integer.
macro_rules! int_enum {
    (
        $(#[$meta:meta])*
        pub enum $name:ident {
            $(
                $(#[$vmeta:meta])*
                $variant:ident = $value:literal
            ),+ $(,)?
        }
    ) => {
        $(#[$meta])*
        #[derive(Debug, Clone, Copy)]
        #[non_exhaustive]
        pub enum $name {
            $(
                $(#[$vmeta])*
                $variant,
            )+
            /// A value this SDK does not recognize. The gateway may add values without notice,
            /// so the raw integer is preserved.
            Unrecognized(i32),
        }

        impl $name {
            /// Every value defined by the specification, in ascending order.
            pub const KNOWN: &'static [$name] = &[$($name::$variant),+];

            /// Converts a raw wire value; unknown values become [`Self::Unrecognized`].
            pub const fn from_value(value: i32) -> Self {
                match value {
                    $($value => $name::$variant,)+
                    other => $name::Unrecognized(other),
                }
            }

            /// Returns the raw wire value.
            pub const fn value(self) -> i32 {
                match self {
                    $($name::$variant => $value,)+
                    $name::Unrecognized(other) => other,
                }
            }

            /// Returns the specification name (`x-enumNames`), or `None` for unrecognized
            /// values.
            pub const fn name(self) -> Option<&'static str> {
                match Self::from_value(self.value()) {
                    $($name::$variant => Some(stringify!($variant)),)+
                    $name::Unrecognized(_) => None,
                }
            }

            /// Returns `true` when the value is defined by the specification.
            pub const fn is_known(self) -> bool {
                self.name().is_some()
            }
        }

        impl $crate::enums::IntEnum for $name {
            const TYPE_NAME: &'static str = stringify!($name);

            fn from_value(value: i32) -> Self {
                $name::from_value(value)
            }

            fn from_name(name: &str) -> Option<Self> {
                $(
                    if name.eq_ignore_ascii_case(stringify!($variant)) {
                        return Some($name::$variant);
                    }
                )+
                None
            }
        }

        impl PartialEq for $name {
            fn eq(&self, other: &Self) -> bool {
                self.value() == other.value()
            }
        }

        impl Eq for $name {}

        impl ::core::hash::Hash for $name {
            fn hash<H: ::core::hash::Hasher>(&self, state: &mut H) {
                self.value().hash(state);
            }
        }

        impl PartialOrd for $name {
            fn partial_cmp(&self, other: &Self) -> Option<::core::cmp::Ordering> {
                Some(self.cmp(other))
            }
        }

        impl Ord for $name {
            fn cmp(&self, other: &Self) -> ::core::cmp::Ordering {
                self.value().cmp(&other.value())
            }
        }

        impl From<i32> for $name {
            fn from(value: i32) -> Self {
                $name::from_value(value)
            }
        }

        impl From<$name> for i32 {
            fn from(value: $name) -> i32 {
                value.value()
            }
        }

        impl ::core::fmt::Display for $name {
            fn fmt(&self, f: &mut ::core::fmt::Formatter<'_>) -> ::core::fmt::Result {
                match self.name() {
                    Some(name) => f.write_str(name),
                    None => write!(f, "Unrecognized({})", self.value()),
                }
            }
        }

        impl ::serde::Serialize for $name {
            fn serialize<S: ::serde::Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
                serializer.serialize_i32(self.value())
            }
        }

        impl<'de> ::serde::Deserialize<'de> for $name {
            fn deserialize<D: ::serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
                deserializer.deserialize_any($crate::enums::IntEnumVisitor::<$name>::new())
            }
        }
    };
}

/// Defines a gateway response envelope: the four common fields plus operation data.
///
/// The optional `manual_debug` marker skips the `Debug` derive for envelopes that carry a
/// secret (a token), which then implement `Debug` by hand. Tokens after the field list are
/// placed inside the generated `GatewayResponse` impl, for hooks such as
/// `failure_order_id`.
macro_rules! gateway_response {
    (
        $(#[$meta:meta])*
        pub struct $name:ident: $code:ident {
            $(
                $(#[$fmeta:meta])*
                pub $field:ident: $fty:ty
            ),* $(,)?
        }
        $($extra:tt)*
    ) => {
        gateway_response! {
            @define [Debug,]
            $(#[$meta])*
            pub struct $name: $code { $( $(#[$fmeta])* pub $field: $fty ),* }
            $($extra)*
        }
    };
    (
        manual_debug
        $(#[$meta:meta])*
        pub struct $name:ident: $code:ident {
            $(
                $(#[$fmeta:meta])*
                pub $field:ident: $fty:ty
            ),* $(,)?
        }
        $($extra:tt)*
    ) => {
        gateway_response! {
            @define []
            $(#[$meta])*
            pub struct $name: $code { $( $(#[$fmeta])* pub $field: $fty ),* }
            $($extra)*
        }
    };
    (
        @define [$($derive:tt)*]
        $(#[$meta:meta])*
        pub struct $name:ident: $code:ident {
            $(
                $(#[$fmeta:meta])*
                pub $field:ident: $fty:ty
            ),*
        }
        $($extra:tt)*
    ) => {
        $(#[$meta])*
        #[derive($($derive)* Clone, PartialEq, ::serde::Serialize, ::serde::Deserialize)]
        #[serde(rename_all = "camelCase")]
        #[non_exhaustive]
        pub struct $name {
            /// `true` when the gateway reports the operation as successful.
            pub success: bool,
            /// Operation-specific result code; zero means success.
            pub error_code: $crate::enums::$code,
            /// Error description. It may be `null` even when the operation failed.
            #[serde(default)]
            pub error_message: Option<String>,
            /// Business-failure flag. Some guide examples omit it, so it defaults to `false`.
            #[serde(default)]
            pub has_business_failure: bool,
            $(
                $(#[$fmeta])*
                pub $field: $fty,
            )*
        }

        impl $crate::operation::GatewayResponse for $name {
            fn success(&self) -> bool {
                self.success
            }

            fn error_code(&self) -> $crate::error::ErrorCode {
                self.error_code.into()
            }

            fn error_message(&self) -> Option<&str> {
                self.error_message.as_deref()
            }

            $($extra)*
        }
    };
}
