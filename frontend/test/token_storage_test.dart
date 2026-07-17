import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hookary/core/services/token_storage.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() => FlutterSecureStorage.setMockInitialValues({}));

  test(
    'the refresh token survives a round trip through the keystore',
    () async {
      final storage = TokenStorage();

      await storage.writeRefreshToken('refresh-abc');

      expect(await storage.readRefreshToken(), 'refresh-abc');
    },
  );

  test('clear forgets both tokens', () async {
    final storage = TokenStorage()..accessToken = 'access-abc';
    await storage.writeRefreshToken('refresh-abc');

    await storage.clear();

    expect(storage.accessToken, isNull);
    expect(await storage.readRefreshToken(), isNull);
  });

  test('writing null deletes the stored token', () async {
    final storage = TokenStorage();
    await storage.writeRefreshToken('refresh-abc');

    await storage.writeRefreshToken(null);

    expect(await storage.readRefreshToken(), isNull);
  });

  test('reading with nothing stored is not an error', () async {
    expect(await TokenStorage().readRefreshToken(), isNull);
  });

  test('native platforms hold the refresh token themselves', () {
    // On web this is false and the token stays in an HttpOnly cookie the app
    // cannot read. These tests run on the VM, so it must be true here.
    expect(TokenStorage().holdsRefreshToken, isTrue);
  });
}
