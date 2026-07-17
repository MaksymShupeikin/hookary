// Hand-written models. No Freezed, no json_serializable, no build_runner —
// see frontend/AGENTS.md.

/// The signed-in user, as /v1/me returns them.
class AuthUser {
  const AuthUser({
    required this.id,
    required this.email,
    required this.displayName,
    this.avatarUrl,
  });

  factory AuthUser.fromJson(Map<String, dynamic> json) => AuthUser(
    id: json['id'] as String,
    email: json['email'] as String,
    displayName: json['display_name'] as String,
    avatarUrl: json['avatar_url'] as String?,
  );

  final String id;
  final String email;
  final String displayName;
  final String? avatarUrl;
}

/// What /v1/auth/google and /v1/auth/refresh return.
class TokenPair {
  const TokenPair({
    required this.accessToken,
    required this.expiresIn,
    this.refreshToken,
  });

  factory TokenPair.fromJson(Map<String, dynamic> json) => TokenPair(
    accessToken: json['access_token'] as String,
    expiresIn: json['expires_in'] as int,
    refreshToken: json['refresh_token'] as String?,
  );

  final String accessToken;
  final int expiresIn;

  /// Null on web, where the server keeps it in an HttpOnly cookie the app
  /// cannot read. That is deliberate, not a missing field.
  final String? refreshToken;
}

/// Where the session stands.
///
/// [unknown] is the state at startup, before the stored session has been
/// checked. It exists so the router can wait instead of flashing the login
/// screen at someone who is already signed in.
enum AuthStatus { unknown, signedOut, signedIn }

class AuthState {
  const AuthState({
    this.status = AuthStatus.unknown,
    this.user,
    this.error,
    this.isBusy = false,
  });

  final AuthStatus status;
  final AuthUser? user;
  final String? error;
  final bool isBusy;

  bool get isSignedIn => status == AuthStatus.signedIn;
  bool get isResolved => status != AuthStatus.unknown;

  AuthState copyWith({
    AuthStatus? status,
    AuthUser? user,
    String? error,
    bool? isBusy,
    bool clearError = false,
    bool clearUser = false,
  }) {
    return AuthState(
      status: status ?? this.status,
      user: clearUser ? null : (user ?? this.user),
      error: clearError ? null : (error ?? this.error),
      isBusy: isBusy ?? this.isBusy,
    );
  }
}
