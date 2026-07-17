import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hookary/core/exceptions/exceptions.dart';
import 'package:hookary/core/services/api_client.dart';
import 'package:hookary/core/services/token_storage.dart';

/// Answers requests from a scripted list, and records what it was asked.
class _ScriptedAdapter implements HttpClientAdapter {
  _ScriptedAdapter(this.responses);

  final List<({int status, Map<String, dynamic> body})> responses;
  final List<RequestOptions> requests = [];

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    requests.add(options);
    final next = responses.removeAt(0);
    return ResponseBody.fromString(
      jsonEncode(next.body),
      next.status,
      headers: {
        Headers.contentTypeHeader: [Headers.jsonContentType],
      },
    );
  }

  @override
  void close({bool force = false}) {}
}

ApiClient _clientWith(_ScriptedAdapter adapter, TokenStorage tokens) {
  final dio = Dio()..httpClientAdapter = adapter;
  return ApiClient(tokens: tokens, dio: dio);
}

void main() {
  test('attaches the access token when there is one', () async {
    final adapter = _ScriptedAdapter([
      (status: 200, body: {'ok': true}),
    ]);
    final tokens = TokenStorage()..accessToken = 'token-abc';

    await _clientWith(adapter, tokens).get<Map<String, dynamic>>('/v1/me');

    expect(
      adapter.requests.single.headers['Authorization'],
      'Bearer token-abc',
    );
  });

  test('sends no Authorization header when signed out', () async {
    final adapter = _ScriptedAdapter([
      (status: 200, body: {'ok': true}),
    ]);

    await _clientWith(
      adapter,
      TokenStorage(),
    ).get<Map<String, dynamic>>('/v1/me');

    expect(
      adapter.requests.single.headers.containsKey('Authorization'),
      isFalse,
    );
  });

  test('surfaces the API error code and request id', () async {
    final adapter = _ScriptedAdapter([
      (
        status: 503,
        body: {
          'code': 'auth_not_configured',
          'message': 'Google Sign-In is not configured on this server.',
          'request_id': '01JZ8XKQ4T6N9R2VBC3D5EFGHJ',
        },
      ),
    ]);

    await expectLater(
      _clientWith(
        adapter,
        TokenStorage(),
      ).post<Map<String, dynamic>>('/v1/auth/google'),
      throwsA(
        isA<ApiException>()
            .having((e) => e.code, 'code', 'auth_not_configured')
            .having(
              (e) => e.requestId,
              'requestId',
              '01JZ8XKQ4T6N9R2VBC3D5EFGHJ',
            ),
      ),
    );
  });

  test('retries once after refreshing on 401', () async {
    final adapter = _ScriptedAdapter([
      (status: 401, body: {'code': 'invalid_token', 'message': 'expired'}),
      (status: 200, body: {'id': 'user-1'}),
    ]);
    final tokens = TokenStorage()..accessToken = 'stale';
    final client = _clientWith(adapter, tokens);

    var refreshes = 0;
    client.onRefreshNeeded = () async {
      refreshes++;
      tokens.accessToken = 'fresh';
      return true;
    };

    final result = await client.get<Map<String, dynamic>>('/v1/me');

    expect(result['id'], 'user-1');
    expect(refreshes, 1);
    expect(adapter.requests.length, 2);
    // The retry must carry the new token, not the one that just failed.
    expect(adapter.requests.last.headers['Authorization'], 'Bearer fresh');
  });

  test('gives up after one refresh rather than looping', () async {
    final adapter = _ScriptedAdapter([
      (status: 401, body: {'code': 'invalid_token', 'message': 'expired'}),
      (
        status: 401,
        body: {'code': 'invalid_token', 'message': 'still expired'},
      ),
    ]);
    final client = _clientWith(adapter, TokenStorage());

    var refreshes = 0;
    client.onRefreshNeeded = () async {
      refreshes++;
      return true;
    };

    await expectLater(
      client.get<Map<String, dynamic>>('/v1/me'),
      throwsA(
        isA<ApiException>().having(
          (e) => e.isUnauthorized,
          'isUnauthorized',
          true,
        ),
      ),
    );
    expect(
      refreshes,
      1,
      reason: 'a second refresh would mean an infinite retry loop',
    );
  });

  test('concurrent 401s trigger only one refresh', () async {
    final adapter = _ScriptedAdapter([
      (status: 401, body: {'code': 'invalid_token', 'message': 'expired'}),
      (status: 401, body: {'code': 'invalid_token', 'message': 'expired'}),
      (status: 200, body: {'id': 'a'}),
      (status: 200, body: {'id': 'b'}),
    ]);
    final client = _clientWith(adapter, TokenStorage());

    var refreshes = 0;
    client.onRefreshNeeded = () async {
      refreshes++;
      await Future<void>.delayed(const Duration(milliseconds: 10));
      return true;
    };

    await Future.wait([
      client.get<Map<String, dynamic>>('/v1/projects'),
      client.get<Map<String, dynamic>>('/v1/me'),
    ]);

    // Each refresh rotates the token, so a second concurrent one would be
    // replaying a revoked token — which the server treats as a leak and kills
    // the whole session.
    expect(refreshes, 1);
  });

  test('does not refresh when nothing knows how to', () async {
    final adapter = _ScriptedAdapter([
      (status: 401, body: {'code': 'invalid_token', 'message': 'expired'}),
    ]);

    await expectLater(
      _clientWith(adapter, TokenStorage()).get<Map<String, dynamic>>('/v1/me'),
      throwsA(isA<ApiException>()),
    );
    expect(adapter.requests.length, 1);
  });
}
