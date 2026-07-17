import 'package:flutter/foundation.dart';

import '../../core/services/api_client.dart';
import 'auth_models.dart';

/// Every auth HTTP call. Widgets and controllers never build a request
/// themselves — see frontend/AGENTS.md.
class AuthApi {
  AuthApi(this._client);

  final ApiClient _client;

  /// Tells the server how this client can hold a refresh token. A browser gets
  /// an HttpOnly cookie; a native app gets the token in the body and puts it in
  /// the keystore.
  static String get _clientKind => kIsWeb ? 'web' : 'mobile';

  /// Exchanges a Google ID token for a Hookary session.
  Future<TokenPair> signInWithGoogle(String idToken) async {
    final json = await _client.post<Map<String, dynamic>>(
      '/v1/auth/google',
      body: {'id_token': idToken, 'client_kind': _clientKind},
    );
    return TokenPair.fromJson(json);
  }

  /// Rotates the session.
  ///
  /// [refreshToken] is null on web, where the cookie travels on its own. Every
  /// call invalidates the previous token, so two concurrent calls are a bug —
  /// ApiClient serialises them.
  Future<TokenPair> refresh(String? refreshToken) async {
    final json = await _client.post<Map<String, dynamic>>(
      '/v1/auth/refresh',
      body: {'client_kind': _clientKind, 'refresh_token': ?refreshToken},
    );
    return TokenPair.fromJson(json);
  }

  /// Revokes the server session even when the access token has just expired.
  ///
  /// Web sends the HttpOnly cookie automatically. Native sends the refresh
  /// token from secure storage; without it an expired access token would make
  /// logout local-only and leave the server session live until its TTL.
  Future<void> logout(String? refreshToken) => _client.post<void>(
    '/v1/auth/logout',
    body: {'client_kind': _clientKind, 'refresh_token': ?refreshToken},
  );

  Future<AuthUser> me() async {
    final json = await _client.get<Map<String, dynamic>>('/v1/me');
    return AuthUser.fromJson(json);
  }
}
