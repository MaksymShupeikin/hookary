import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_sign_in/google_sign_in.dart';
import 'package:hookary/core/services/api_client.dart';
import 'package:hookary/core/services/google_auth_client.dart';
import 'package:hookary/core/exceptions/exceptions.dart';
import 'package:hookary/core/services/token_storage.dart';
import 'package:hookary/features/auth/auth_api.dart';
import 'package:hookary/features/auth/auth_controller.dart';
import 'package:hookary/features/auth/auth_models.dart';

const _user = AuthUser(
  id: '01JZ8XKQ4T6N9R2VBC3D5EFGHJ',
  email: 'ada@example.com',
  displayName: 'Ada',
);

class _FakeAuthApi extends AuthApi {
  _FakeAuthApi() : super(ApiClient(tokens: TokenStorage()));

  ApiException? refreshFails;
  ApiException? logoutFails;
  int refreshCalls = 0;
  int exchangeCalls = 0;
  int logoutCalls = 0;
  String? logoutRefreshToken;

  @override
  Future<TokenPair> refresh(String? refreshToken) async {
    refreshCalls++;
    final failure = refreshFails;
    if (failure != null) throw failure;
    return const TokenPair(
      accessToken: 'access',
      expiresIn: 900,
      refreshToken: 'refresh',
    );
  }

  @override
  Future<TokenPair> signInWithGoogle(String idToken) async {
    exchangeCalls++;
    return const TokenPair(
      accessToken: 'access',
      expiresIn: 900,
      refreshToken: 'refresh',
    );
  }

  @override
  Future<void> logout(String? refreshToken) async {
    logoutCalls++;
    logoutRefreshToken = refreshToken;
    final failure = logoutFails;
    if (failure != null) throw failure;
  }

  @override
  Future<AuthUser> me() async => _user;
}

class _FakeGoogle extends GoogleAuthClient {
  _FakeGoogle({this.silentToken});

  final String? silentToken;
  int silentCalls = 0;
  int signOutCalls = 0;

  final _events = StreamController<GoogleSignInAuthenticationEvent>.broadcast();

  @override
  Future<void> initialize() async {}

  @override
  bool get supportsDirectAuthentication => true;

  @override
  Stream<GoogleSignInAuthenticationEvent> get events => _events.stream;

  @override
  Future<String?> attemptSilent() async {
    silentCalls++;
    return silentToken;
  }

  @override
  Future<void> signOut() async => signOutCalls++;
}

ProviderContainer _containerWith(_FakeAuthApi api, _FakeGoogle google) {
  final container = ProviderContainer(
    overrides: [
      authApiProvider.overrideWithValue(api),
      googleAuthClientProvider.overrideWithValue(google),
    ],
  );
  addTearDown(container.dispose);
  return container;
}

ApiException unauthorized() =>
    ApiException(statusCode: 401, code: 'invalid_token', message: 'no session');

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() => FlutterSecureStorage.setMockInitialValues({}));

  test('our own session wins, and Google is never asked', () async {
    final api = _FakeAuthApi();
    final google = _FakeGoogle(silentToken: 'google-token');
    final container = _containerWith(api, google);
    await container
        .read(tokenStorageProvider)
        .writeRefreshToken('stored-refresh');

    await container.read(authControllerProvider.notifier).restoreSession();

    final state = container.read(authControllerProvider);
    expect(state.status, AuthStatus.signedIn);
    expect(state.user?.email, 'ada@example.com');
    // Asking Google when we already have a session would raise One Tap over an
    // app the user is already signed in to.
    expect(google.silentCalls, 0);
  });

  test('falls back to Google when there is no session of ours', () async {
    final api = _FakeAuthApi()..refreshFails = unauthorized();
    final google = _FakeGoogle(silentToken: 'google-token');
    final container = _containerWith(api, google);
    await container
        .read(tokenStorageProvider)
        .writeRefreshToken('stale-refresh');

    await container.read(authControllerProvider.notifier).restoreSession();

    expect(google.silentCalls, 1);
    expect(api.exchangeCalls, 1);
    expect(container.read(authControllerProvider).status, AuthStatus.signedIn);
  });

  test('resolves to signed out when Google knows nobody either', () async {
    final api = _FakeAuthApi()..refreshFails = unauthorized();
    final google = _FakeGoogle();
    final container = _containerWith(api, google);

    await container.read(authControllerProvider.notifier).restoreSession();

    final state = container.read(authControllerProvider);
    // Must resolve: the router waits on this, and an unresolved state is a
    // splash screen that never ends.
    expect(state.isResolved, isTrue);
    expect(state.status, AuthStatus.signedOut);
    expect(state.user, isNull);
  });

  test('a fresh visitor with nothing stored never calls refresh', () async {
    final api = _FakeAuthApi();
    final google = _FakeGoogle();
    final container = _containerWith(api, google);

    await container.read(authControllerProvider.notifier).restoreSession();

    expect(
      api.refreshCalls,
      0,
      reason: 'no stored token means nothing to rotate',
    );
    expect(container.read(authControllerProvider).status, AuthStatus.signedOut);
  });

  test('a network failure is not a logout', () async {
    final api = _FakeAuthApi()
      ..refreshFails = ApiException(
        statusCode: 0,
        code: 'network_error',
        message: 'offline',
      );
    final google = _FakeGoogle();
    final container = _containerWith(api, google);
    await container
        .read(tokenStorageProvider)
        .writeRefreshToken('stored-refresh');

    await container.read(authControllerProvider.notifier).restoreSession();

    // The token must survive: a flaky connection must not evict the user.
    expect(
      await container.read(tokenStorageProvider).readRefreshToken(),
      'stored-refresh',
    );
    expect(container.read(authControllerProvider).isResolved, isTrue);
  });

  test('signing out clears the stored token and the Google session', () async {
    final api = _FakeAuthApi();
    final google = _FakeGoogle();
    final container = _containerWith(api, google);
    final tokens = container.read(tokenStorageProvider);
    await tokens.writeRefreshToken('stored-refresh');
    tokens.accessToken = 'access';

    await container.read(authControllerProvider.notifier).signOut();

    expect(api.logoutCalls, 1);
    expect(api.logoutRefreshToken, 'stored-refresh');
    expect(
      google.signOutCalls,
      1,
      reason: 'otherwise One Tap offers the identity straight back',
    );
    expect(tokens.accessToken, isNull);
    expect(await tokens.readRefreshToken(), isNull);
    expect(container.read(authControllerProvider).status, AuthStatus.signedOut);
  });

  test('signs out locally even when the server cannot be reached', () async {
    final api = _FakeAuthApi()
      ..logoutFails = ApiException(
        statusCode: 0,
        code: 'network_error',
        message: 'offline',
      );
    final google = _FakeGoogle();
    final container = _containerWith(api, google);
    await container
        .read(tokenStorageProvider)
        .writeRefreshToken('stored-refresh');

    // Refusing to sign someone out because the network is down would strand
    // them in an app they asked to leave.
    await container.read(authControllerProvider.notifier).signOut();

    expect(api.logoutCalls, 1);
    expect(container.read(authControllerProvider).status, AuthStatus.signedOut);
    expect(
      await container.read(tokenStorageProvider).readRefreshToken(),
      isNull,
    );
  });
}
