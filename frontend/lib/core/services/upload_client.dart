import 'upload_client_io.dart'
    if (dart.library.js_interop) 'upload_client_web.dart'
    as platform;
import 'upload_client_types.dart';

export '../exceptions/exceptions.dart';
export 'upload_client_types.dart';

/// Sends one whole object or one multipart slice directly to object storage.
///
/// The platform implementations preserve the same invariant in different
/// ways: native streams from the file, while web sends a browser Blob slice.
/// Neither turns the complete video into a Dart Uint8List.
class UploadClient {
  UploadClient() : _transport = platform.UploadTransport();

  final platform.UploadTransport _transport;

  Future<String?> put(UploadRequest request) => _transport.put(request);
}
