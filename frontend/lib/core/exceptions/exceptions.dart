/// Core custom exception classes for Hookary frontend.
library;

/// An error the API reported, in the shape contracts/openapi.yaml defines.
///
/// [code] is stable and meant to be branched on; [message] is for people.
class ApiException implements Exception {
  ApiException({
    required this.statusCode,
    required this.code,
    required this.message,
    this.requestId,
  });

  final int statusCode;
  final String code;
  final String message;
  final String? requestId;

  bool get isUnauthorized => statusCode == 401;

  @override
  String toString() =>
      'ApiException($statusCode, $code${requestId == null ? '' : ', $requestId'}): $message';
}

class UploadStoppedException implements Exception {
  const UploadStoppedException({required this.paused});

  final bool paused;
}

class UploadTransportException implements Exception {
  const UploadTransportException(this.message, {this.statusCode});

  final String message;
  final int? statusCode;

  @override
  String toString() => message;
}
