import 'package:flutter/foundation.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

/// Where each token lives.
///
/// The access token is held in memory and nowhere else: it is short-lived, and
/// anything that persists it only widens the window in which it can be stolen.
///
/// The refresh token is long-lived and is treated differently per platform:
///
///   * web — the server puts it in an HttpOnly cookie. This class never sees
///     it, which is the point: JavaScript cannot read it, so an XSS cannot
///     walk away with the session.
///   * iOS/Android — the platform keystore, via flutter_secure_storage. Never
///     SharedPreferences, which is plain text on disk.
class TokenStorage {
  TokenStorage({FlutterSecureStorage? secureStorage})
    : _secure = secureStorage ?? const FlutterSecureStorage();

  static const _refreshTokenKey = 'hookary_refresh_token';

  final FlutterSecureStorage _secure;

  /// Memory only, and deliberately not persisted anywhere.
  String? accessToken;

  /// True when this platform hands the refresh token to the app at all.
  bool get holdsRefreshToken => !kIsWeb;

  Future<String?> readRefreshToken() async {
    if (!holdsRefreshToken) return null;
    return _secure.read(key: _refreshTokenKey);
  }

  Future<void> writeRefreshToken(String? token) async {
    if (!holdsRefreshToken) return;
    if (token == null) {
      await _secure.delete(key: _refreshTokenKey);
      return;
    }
    await _secure.write(key: _refreshTokenKey, value: token);
  }

  /// Forgets everything. Safe to call when nothing is stored.
  Future<void> clear() async {
    accessToken = null;
    if (holdsRefreshToken) {
      await _secure.delete(key: _refreshTokenKey);
    }
  }
}
