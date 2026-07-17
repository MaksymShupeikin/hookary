import 'package:file_selector/file_selector.dart';

export '../exceptions/exceptions.dart';

typedef UploadProgress = void Function(int sentBytes, int totalBytes);

class UploadRequest {
  const UploadRequest({
    required this.file,
    required this.url,
    required this.start,
    required this.end,
    required this.contentType,
    required this.cancellation,
    required this.onProgress,
  });

  final XFile file;
  final Uri url;
  final int start;
  final int end;
  final String contentType;
  final UploadCancellation cancellation;
  final UploadProgress onProgress;

  int get length => end - start;
}

class UploadCancellation {
  final Set<void Function()> _aborters = {};
  bool _paused = false;
  bool _cancelled = false;

  bool get isPaused => _paused;
  bool get isCancelled => _cancelled;
  bool get isStopped => _paused || _cancelled;

  void pause() {
    if (_cancelled) return;
    _paused = true;
    _abortActive();
  }

  void cancel() {
    _cancelled = true;
    _paused = false;
    _abortActive();
  }

  void attach(void Function() abort) {
    if (isStopped) {
      abort();
      return;
    }
    _aborters.add(abort);
  }

  void detach(void Function() abort) => _aborters.remove(abort);

  void _abortActive() {
    final active = List<void Function()>.of(_aborters);
    _aborters.clear();
    for (final abort in active) {
      abort();
    }
  }
}
