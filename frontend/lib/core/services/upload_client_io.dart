import 'dart:io';

import 'upload_client_types.dart';

class UploadTransport {
  Future<String?> put(UploadRequest request) async {
    if (request.cancellation.isStopped) {
      throw UploadStoppedException(paused: request.cancellation.isPaused);
    }

    final client = HttpClient();
    HttpClientRequest? active;
    void abort() => active?.abort();
    request.cancellation.attach(abort);

    try {
      active = await client.openUrl('PUT', request.url);
      active.contentLength = request.length;
      active.headers.contentType = ContentType.parse(request.contentType);

      var sent = 0;
      await for (final chunk in request.file.openRead(
        request.start,
        request.end,
      )) {
        if (request.cancellation.isStopped) {
          active.abort();
          throw UploadStoppedException(paused: request.cancellation.isPaused);
        }
        active.add(chunk);
        sent += chunk.length;
        request.onProgress(sent, request.length);
      }

      final response = await active.close();
      await response.drain<void>();
      if (response.statusCode < 200 || response.statusCode >= 300) {
        throw UploadTransportException(
          'Object storage rejected the upload.',
          statusCode: response.statusCode,
        );
      }
      return response.headers.value(HttpHeaders.etagHeader);
    } on UploadStoppedException {
      rethrow;
    } on Object catch (error) {
      if (request.cancellation.isStopped) {
        throw UploadStoppedException(paused: request.cancellation.isPaused);
      }
      if (error is UploadTransportException) rethrow;
      throw const UploadTransportException(
        'Could not upload to object storage.',
      );
    } finally {
      request.cancellation.detach(abort);
      client.close(force: true);
    }
  }
}
