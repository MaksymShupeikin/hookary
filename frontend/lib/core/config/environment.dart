/// Build-time configuration.
///
/// Values come from `--dart-define`, so a web build is a plain static bundle
/// with no runtime config fetch. Defaults point at the local compose stack.
///
///     flutter run --dart-define=HOOKARY_API_BASE_URL=https://api.hookary.com
library;

class Environment {
  const Environment._();

  static const String apiBaseUrl = String.fromEnvironment(
    'HOOKARY_API_BASE_URL',
    defaultValue: 'http://localhost:8080',
  );

  /// Derived from [apiBaseUrl] rather than defined separately: the two pointing
  /// at different hosts is always a mistake, so it should not be expressible.
  static String get wsBaseUrl =>
      apiBaseUrl.replaceFirst(RegExp(r'^http'), 'ws');

  /// Audience for Google ID tokens on web. The API verifies against the
  /// matching id; a mismatch fails the token check rather than logging anyone
  /// in, so this must be set for real builds.
  static const String googleClientIdWeb = String.fromEnvironment(
    'HOOKARY_GOOGLE_CLIENT_ID_WEB',
  );

  static const bool isProduction = bool.fromEnvironment(
    'HOOKARY_PRODUCTION',
    defaultValue: false,
  );
}
