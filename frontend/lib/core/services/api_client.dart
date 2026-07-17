import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';

import '../config/environment.dart';
import '../exceptions/exceptions.dart';
import 'dio_credentials_stub.dart'
    if (dart.library.js_interop) 'dio_credentials_web.dart';
import 'token_storage.dart';

/// The HTTP client every feature's API class talks through.
///
/// It owns two things no caller should repeat: attaching the access token, and
/// refreshing it once when it has expired.
class ApiClient {
  ApiClient({required this.tokens, Dio? dio}) : _dio = dio ?? Dio() {
    _dio.options
      ..baseUrl = Environment.apiBaseUrl
      ..connectTimeout = const Duration(seconds: 15)
      // Generous, because these are API calls; media never comes through here.
      ..receiveTimeout = const Duration(seconds: 30)
      ..contentType = Headers.jsonContentType
      // Every status reaches _send, which turns the body into an ApiException.
      // Letting Dio throw on 5xx instead would bury a real answer — a 503
      // auth_not_configured, say — under a generic transport error.
      ..validateStatus = (_) => true;

    enableCookieCredentials(_dio);
    _dio.interceptors.add(InterceptorsWrapper(onRequest: _attachAccessToken));
  }

  final Dio _dio;

  /// Where the access token this client attaches comes from.
  final TokenStorage tokens;

  /// Refreshes the session. Set by AuthController, which owns what a refresh
  /// means; the client only knows when to ask for one.
  Future<bool> Function()? onRefreshNeeded;

  /// Guards against a stampede: when several requests expire together, they
  /// must not each start their own refresh, because every refresh rotates the
  /// token and the losers would be replaying a revoked one — which the server
  /// treats as a leak and kills the whole session.
  Future<bool>? _refreshInFlight;

  Dio get raw => _dio;

  void _attachAccessToken(
    RequestOptions options,
    RequestInterceptorHandler handler,
  ) {
    final token = tokens.accessToken;
    if (token != null) {
      options.headers['Authorization'] = 'Bearer $token';
    }
    handler.next(options);
  }

  Future<T> get<T>(String path, {Map<String, dynamic>? query}) =>
      _send<T>(() => _dio.get<dynamic>(path, queryParameters: query));

  Future<T> post<T>(String path, {Object? body, Options? options}) =>
      _send<T>(() => _dio.post<dynamic>(path, data: body, options: options));

  Future<T> patch<T>(String path, {Object? body}) =>
      _send<T>(() => _dio.patch<dynamic>(path, data: body));

  Future<T> delete<T>(String path) =>
      _send<T>(() => _dio.delete<dynamic>(path));

  /// Sends a request, refreshing the session once if it comes back 401.
  ///
  /// [allowRefresh] stops the recursion: the refresh call itself must never
  /// trigger another refresh.
  Future<T> _send<T>(
    Future<Response<dynamic>> Function() send, {
    bool allowRefresh = true,
  }) async {
    Response<dynamic> response;
    try {
      response = await send();
    } on DioException catch (e) {
      throw ApiException(
        statusCode: e.response?.statusCode ?? 0,
        code: 'network_error',
        message: 'Could not reach Hookary. Check your connection.',
      );
    }

    final path = response.requestOptions.path;
    if (response.statusCode == 401 &&
        allowRefresh &&
        onRefreshNeeded != null &&
        !path.startsWith('/v1/auth/')) {
      if (await _refreshOnce()) {
        return _send<T>(send, allowRefresh: false);
      }
    }

    if (response.statusCode! >= 400) {
      throw _errorFrom(response);
    }
    return response.data as T;
  }

  Future<bool> _refreshOnce() {
    final existing = _refreshInFlight;
    if (existing != null) return existing;

    final attempt = onRefreshNeeded!().whenComplete(() {
      _refreshInFlight = null;
    });
    _refreshInFlight = attempt;
    return attempt;
  }

  ApiException _errorFrom(Response<dynamic> response) {
    final data = response.data;
    if (data is Map<String, dynamic> && data['code'] is String) {
      return ApiException(
        statusCode: response.statusCode!,
        code: data['code'] as String,
        message: data['message'] as String? ?? 'Something went wrong.',
        requestId: data['request_id'] as String?,
      );
    }

    // A non-JSON error means something in front of the API answered — a proxy,
    // a load balancer. Its body is not ours to show.
    debugPrint('Unrecognised error body from ${response.realUri}');
    return ApiException(
      statusCode: response.statusCode!,
      code: 'unexpected_response',
      message: 'Something went wrong.',
    );
  }
}
