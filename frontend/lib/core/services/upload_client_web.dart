import 'dart:async';
import 'dart:js_interop';

import 'package:web/web.dart' as web;

import 'upload_client_types.dart';

class UploadTransport {
  String? _cachedObjectUrl;
  Future<web.Blob>? _cachedBlob;

  Future<String?> put(UploadRequest request) async {
    if (request.cancellation.isStopped) {
      throw UploadStoppedException(paused: request.cancellation.isPaused);
    }

    final blob = await _blobFor(request.file.path);
    final slice = blob.slice(request.start, request.end, request.contentType);
    final xhr = web.XMLHttpRequest()..open('PUT', request.url.toString(), true);
    xhr.setRequestHeader('Content-Type', request.contentType);

    final result = Completer<String?>();
    var finished = false;

    void completeValue(String? value) {
      if (finished) return;
      finished = true;
      result.complete(value);
    }

    void completeError(Object error) {
      if (finished) return;
      finished = true;
      result.completeError(error);
    }

    void abort() => xhr.abort();
    request.cancellation.attach(abort);

    xhr.upload.addEventListener(
      'progress',
      ((web.Event event) {
        final progress = event as web.ProgressEvent;
        request.onProgress(progress.loaded, request.length);
      }).toJS,
    );
    xhr.addEventListener(
      'load',
      ((web.Event _) {
        if (xhr.status >= 200 && xhr.status < 300) {
          completeValue(xhr.getResponseHeader('ETag'));
        } else {
          completeError(
            UploadTransportException(
              'Object storage rejected the upload.',
              statusCode: xhr.status,
            ),
          );
        }
      }).toJS,
    );
    xhr.addEventListener(
      'error',
      ((web.Event _) {
        completeError(
          const UploadTransportException('Could not upload to object storage.'),
        );
      }).toJS,
    );
    xhr.addEventListener(
      'abort',
      ((web.Event _) {
        completeError(
          UploadStoppedException(paused: request.cancellation.isPaused),
        );
      }).toJS,
    );

    xhr.send(slice);
    try {
      return await result.future;
    } finally {
      request.cancellation.detach(abort);
    }
  }

  Future<web.Blob> _blobFor(String objectUrl) {
    if (_cachedObjectUrl == objectUrl && _cachedBlob != null) {
      return _cachedBlob!;
    }

    _cachedObjectUrl = objectUrl;
    final future = _loadAndCacheBlob(objectUrl);
    _cachedBlob = future;
    return future;
  }

  Future<web.Blob> _loadAndCacheBlob(String objectUrl) async {
    try {
      return await _loadBlob(objectUrl);
    } on Object {
      if (_cachedObjectUrl == objectUrl) {
        _cachedObjectUrl = null;
        _cachedBlob = null;
      }
      rethrow;
    }
  }

  Future<web.Blob> _loadBlob(String objectUrl) {
    final result = Completer<web.Blob>();
    final xhr = web.XMLHttpRequest()
      ..open('GET', objectUrl, true)
      ..responseType = 'blob';

    xhr.addEventListener(
      'load',
      ((web.Event _) {
        final response = xhr.response;
        if (response == null) {
          result.completeError(
            const UploadTransportException(
              'The selected file is no longer available.',
            ),
          );
          return;
        }
        result.complete(response as web.Blob);
      }).toJS,
    );
    xhr.addEventListener(
      'error',
      ((web.Event _) {
        result.completeError(
          const UploadTransportException(
            'The selected file could not be opened.',
          ),
        );
      }).toJS,
    );
    xhr.send();
    return result.future;
  }
}
