import 'package:dio/dio.dart';

/// Native platforms have no cookie jar to opt into: the refresh token is read
/// from secure storage and sent in the body instead.
void enableCookieCredentials(Dio dio) {}
