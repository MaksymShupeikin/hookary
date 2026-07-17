import 'package:dio/browser.dart';
import 'package:dio/dio.dart';

/// Lets the browser attach the HttpOnly refresh cookie to auth requests.
///
/// Without this, cross-origin XHR sends no cookies at all and /v1/auth/refresh
/// sees an anonymous request, so every reload would log the user out. The
/// server must also allow credentials for the app's origin, which is what
/// CORS_ALLOWED_ORIGINS drives.
void enableCookieCredentials(Dio dio) {
  final adapter = dio.httpClientAdapter;
  if (adapter is BrowserHttpClientAdapter) {
    adapter.withCredentials = true;
  }
}
