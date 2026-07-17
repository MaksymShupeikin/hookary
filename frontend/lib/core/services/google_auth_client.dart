import 'package:flutter/foundation.dart';
import 'package:google_sign_in/google_sign_in.dart';

import '../config/environment.dart';

/// Wraps Google Sign-In so the rest of the app only ever deals in ID tokens.
///
/// Nothing here logs anyone in. It obtains a Google ID token, which the Hookary
/// API verifies and exchanges for a session of its own — the app never trusts
/// this token, and neither should any caller.
class GoogleAuthClient {
  GoogleAuthClient({GoogleSignIn? signIn})
    : _signIn = signIn ?? GoogleSignIn.instance;

  final GoogleSignIn _signIn;
  bool _initialized = false;

  /// Login asks for identity and nothing else. Uploading to YouTube is a
  /// separate grant, requested later and only if the user publishes; bundling
  /// it here would demand channel access from someone who just wants to sign in.
  static const List<String> loginScopes = <String>['email', 'profile'];

  Future<void> initialize() async {
    if (_initialized) return;

    await _signIn.initialize(
      // On web the client id identifies the app to Google. On Android it comes
      // from the SHA-1 registered in the Cloud console, so passing one there
      // would be ignored at best.
      clientId: kIsWeb ? _requireWebClientId() : null,
      // The audience the backend verifies against. Without it, Android and iOS
      // return an ID token addressed to the platform client, which the API
      // would rightly reject.
      serverClientId: kIsWeb ? null : _requireWebClientId(),
    );
    _initialized = true;
  }

  /// Whether this platform can start sign-in from our own button.
  ///
  /// Web cannot: Google requires its own rendered button there, so the UI has
  /// to show that instead. See [buildGoogleSignInButton].
  bool get supportsDirectAuthentication => _signIn.supportsAuthenticate();

  /// Events for sign-in and sign-out, including the ones the web button raises.
  Stream<GoogleSignInAuthenticationEvent> get events =>
      _signIn.authenticationEvents;

  /// Signs in interactively. Only valid where [supportsDirectAuthentication].
  Future<String?> authenticate() async {
    await initialize();
    final account = await _signIn.authenticate(scopeHint: loginScopes);
    return account.authentication.idToken;
  }

  /// Asks Google to identify the user without an interactive sign-in.
  ///
  /// The two platforms answer differently, and the difference is not cosmetic:
  ///
  ///   * native — returns an ID token, or null when there is no Google session.
  ///     A first-time user hitting null is normal, not an error.
  ///   * web — returns null *immediately* and raises the One Tap prompt.
  ///     The platform hands back no future at all ("may never return, so
  ///     clients should not await it"), and the answer arrives on [events]
  ///     whenever the user acts on the prompt. So null here means "no answer
  ///     yet", never "no session" — treating it as a definite no would leave
  ///     One Tap on screen with nothing listening for the result.
  Future<String?> attemptSilent() async {
    await initialize();

    final pending = _signIn.attemptLightweightAuthentication();
    if (pending == null) {
      return null;
    }
    final account = await pending;
    return account?.authentication.idToken;
  }

  /// Ends the Google session.
  ///
  /// On web this also tells GIS to stop auto-selecting the account, which is
  /// what keeps One Tap from immediately offering back the identity the user
  /// just signed out of.
  Future<void> signOut() async {
    if (!_initialized) return;
    await _signIn.signOut();
  }

  static String _requireWebClientId() {
    const id = Environment.googleClientIdWeb;
    if (id.isEmpty) {
      throw StateError(
        'HOOKARY_GOOGLE_CLIENT_ID_WEB is not set. Pass it at build time:\n'
        '  flutter run --dart-define=HOOKARY_GOOGLE_CLIENT_ID_WEB=xxx.apps.googleusercontent.com',
      );
    }
    return id;
  }
}
