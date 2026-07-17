import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:google_sign_in/google_sign_in.dart';

import '../../core/exceptions/exceptions.dart';
import '../../core/services/api_client.dart';
import '../../core/services/google_auth_client.dart';
import '../../core/services/token_storage.dart';
import 'auth_api.dart';
import 'auth_models.dart';

/// Providers are written by hand. No riverpod_generator, no build_runner.

final tokenStorageProvider = Provider<TokenStorage>((ref) => TokenStorage());

final apiClientProvider = Provider<ApiClient>((ref) {
  return ApiClient(tokens: ref.watch(tokenStorageProvider));
});

final authApiProvider = Provider<AuthApi>((ref) {
  return AuthApi(ref.watch(apiClientProvider));
});

final googleAuthClientProvider = Provider<GoogleAuthClient>((ref) {
  return GoogleAuthClient();
});

final authControllerProvider = NotifierProvider<AuthController, AuthState>(
  AuthController.new,
);

/// Owns the session: restoring it, starting it, ending it.
///
/// Loading and error state live here rather than in the widgets, so every
/// screen shows the same thing for the same situation.
class AuthController extends Notifier<AuthState> {
  StreamSubscription<GoogleSignInAuthenticationEvent>? _googleEvents;

  @override
  AuthState build() {
    final client = ref.watch(apiClientProvider);

    // The client knows a 401 means "refresh"; only this controller knows what
    // refreshing involves.
    client.onRefreshNeeded = _refreshSession;

    ref.onDispose(() {
      client.onRefreshNeeded = null;
      _googleEvents?.cancel();
    });

    return const AuthState();
  }

  TokenStorage get _tokens => ref.read(tokenStorageProvider);
  AuthApi get _api => ref.read(authApiProvider);
  GoogleAuthClient get _google => ref.read(googleAuthClientProvider);

  /// Restores a session at startup, if there is one.
  ///
  /// Two things can identify the user, in this order:
  ///
  ///   1. our own refresh token — the real session, and the only one that
  ///      survives a Google sign-out;
  ///   2. Google itself, silently — which on web raises One Tap, so a returning
  ///      visitor signs in without a popup or even a click.
  ///
  /// Leaves the state resolved either way: the router waits on this, and a
  /// state that never resolves is a splash screen that never ends.
  Future<void> restoreSession() async {
    // Subscribe before anything can raise One Tap, or its answer lands with
    // nobody listening.
    try {
      await _listenForWebSignIn();
    } on Object catch (_) {
      // Google being unconfigured must not block startup. Sign-in itself will
      // report the problem, where the user can see it.
    }

    if (await _refreshSession()) {
      return;
    }

    // No Hookary session. Ask Google whether it already knows this person.
    try {
      final idToken = await _google.attemptSilent();
      if (idToken != null) {
        await _exchange(idToken);
        return;
      }
    } on Object catch (_) {
      // No Google session, or Google is not configured. Either way, this is a
      // signed-out visitor, not a failure worth showing.
    }

    // On web the One Tap prompt may still be on screen; if the user takes it,
    // the event listener above flips this to signed in. Resolving to signed out
    // now is what puts the sign-in page — and its button — underneath it.
    state = state.copyWith(status: AuthStatus.signedOut, clearUser: true);
  }

  /// Starts an interactive sign-in.
  ///
  /// On web this is unreachable: Google requires its own rendered button there,
  /// and the result arrives through [_listenForWebSignIn] instead.
  Future<void> signInWithGoogle() async {
    state = state.copyWith(isBusy: true, clearError: true);
    try {
      final idToken = await _google.authenticate();
      if (idToken == null) {
        state = state.copyWith(
          isBusy: false,
          error: 'Google did not return an identity token.',
        );
        return;
      }
      await _exchange(idToken);
    } on GoogleSignInException catch (e) {
      // Cancelling is not an error; saying so would be noise.
      if (e.code == GoogleSignInExceptionCode.canceled) {
        state = state.copyWith(isBusy: false);
        return;
      }
      state = state.copyWith(isBusy: false, error: 'Google sign-in failed.');
    } on ApiException catch (e) {
      state = state.copyWith(isBusy: false, error: _messageFor(e));
    }
  }

  Future<void> signOut() async {
    state = state.copyWith(isBusy: true, clearError: true);
    try {
      await _api.logout(await _tokens.readRefreshToken());
    } on ApiException catch (_) {
      // Sign out locally regardless: refusing to log someone out because the
      // network is down is never the right answer.
    }
    await _google.signOut();
    await _tokens.clear();
    state = const AuthState(status: AuthStatus.signedOut);
  }

  /// Picks up sign-ins raised by Google's own button on web.
  Future<void> _listenForWebSignIn() async {
    await _google.initialize();
    if (_google.supportsDirectAuthentication) return;

    _googleEvents ??= _google.events.listen(
      (event) async {
        if (event is! GoogleSignInAuthenticationEventSignIn) return;

        final idToken = event.user.authentication.idToken;
        if (idToken == null) return;

        state = state.copyWith(isBusy: true, clearError: true);
        try {
          await _exchange(idToken);
        } on ApiException catch (e) {
          state = state.copyWith(isBusy: false, error: _messageFor(e));
        }
      },
      onError: (Object _) {
        state = state.copyWith(isBusy: false, error: 'Google sign-in failed.');
      },
    );
  }

  /// Trades a Google ID token for a Hookary session.
  Future<void> _exchange(String idToken) async {
    final pair = await _api.signInWithGoogle(idToken);
    await _storeTokens(pair);

    final user = await _api.me();
    state = AuthState(status: AuthStatus.signedIn, user: user);
  }

  /// Rotates the session and reports whether one now exists.
  ///
  /// Returns false rather than throwing on 401: no session is an ordinary
  /// answer here, not a failure.
  Future<bool> _refreshSession() async {
    try {
      final stored = await _tokens.readRefreshToken();
      if (_tokens.holdsRefreshToken && stored == null) {
        return false;
      }

      final pair = await _api.refresh(stored);
      await _storeTokens(pair);

      final user = await _api.me();
      state = AuthState(status: AuthStatus.signedIn, user: user);
      return true;
    } on ApiException catch (e) {
      if (e.isUnauthorized) {
        await _tokens.clear();
        return false;
      }
      // A network failure is not a logout. Report it and keep whatever session
      // is already known, so a flaky connection does not evict the user.
      state = state.copyWith(
        status: state.isResolved ? state.status : AuthStatus.signedOut,
        error: _messageFor(e),
        isBusy: false,
      );
      return false;
    }
  }

  Future<void> _storeTokens(TokenPair pair) async {
    _tokens.accessToken = pair.accessToken;
    // Null on web, where the cookie is the only copy and the app cannot see it.
    if (pair.refreshToken != null) {
      await _tokens.writeRefreshToken(pair.refreshToken);
    }
  }

  String _messageFor(ApiException e) {
    return switch (e.code) {
      'auth_not_configured' =>
        'Google Sign-In is not set up on the server yet.',
      'invalid_token' => 'That sign-in could not be verified. Try again.',
      'network_error' => 'Could not reach Hookary. Check your connection.',
      _ => e.message,
    };
  }
}
