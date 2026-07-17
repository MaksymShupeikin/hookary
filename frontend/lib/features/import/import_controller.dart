import 'dart:math' as math;

import 'package:file_selector/file_selector.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/services/upload_client.dart';
import '../projects/projects_controller.dart';
import 'import_api.dart';
import 'import_models.dart';
import 'sources_controller.dart';

const _parallelParts = 3;
const _maxPartAttempts = 3;

final uploadClientProvider = Provider<UploadClient>((ref) => UploadClient());

final importControllerProvider =
    NotifierProvider<ImportController, UploadState>(ImportController.new);

class ImportController extends Notifier<UploadState> {
  XFile? _file;
  UploadCancellation? _cancellation;
  final Map<int, UploadedPart> _completedParts = {};
  final Map<int, int> _activePartBytes = {};

  @override
  UploadState build() {
    ref.onDispose(() => _cancellation?.cancel());
    return const UploadState();
  }

  ImportApi get _api => ref.read(importApiProvider);
  UploadClient get _uploads => ref.read(uploadClientProvider);

  Future<void> chooseAndUpload(String projectId) async {
    if (state.isActive) return;
    state = UploadState(phase: UploadPhase.choosing, projectId: projectId);

    final file = await openFile(
      acceptedTypeGroups: const [
        XTypeGroup(
          label: 'Video',
          extensions: ['mp4', 'mov', 'webm', 'mkv', 'mpeg', 'mpg'],
          mimeTypes: [
            'video/mp4',
            'video/quicktime',
            'video/webm',
            'video/x-matroska',
            'video/mpeg',
          ],
        ),
      ],
    );
    if (file == null) {
      state = const UploadState();
      return;
    }

    try {
      final size = await file.length();
      final contentType = _contentType(file);
      final session = await _api.startUpload(
        projectId: projectId,
        filename: file.name,
        contentType: contentType,
        sizeBytes: size,
      );

      _file = file;
      _completedParts
        ..clear()
        ..addEntries(
          session.uploadedParts.map((part) => MapEntry(part.partNumber, part)),
        );
      state = UploadState(
        phase: UploadPhase.uploading,
        projectId: projectId,
        filename: file.name,
        sizeBytes: size,
        session: session,
      );
      await _continueUpload(session);
    } on ApiException catch (error) {
      _fail(error.message);
    } on UploadTransportException catch (error) {
      _fail(error.message);
    } on Object {
      _fail('Could not start that upload.');
    }
  }

  Future<void> importYouTube(String projectId, String rawUrl) async {
    if (state.isActive) return;
    final url = rawUrl.trim();
    final parsed = Uri.tryParse(url);
    if (parsed == null ||
        !parsed.hasScheme ||
        !parsed.hasAuthority ||
        (parsed.scheme != 'http' && parsed.scheme != 'https') ||
        !_isYouTubeHost(parsed.host)) {
      state = UploadState(
        phase: UploadPhase.failed,
        projectId: projectId,
        sourceUrl: url,
        error: 'Enter a public YouTube link.',
      );
      return;
    }

    state = UploadState(
      phase: UploadPhase.importingUrl,
      projectId: projectId,
      sourceUrl: url,
    );
    try {
      final job = await _api.startYouTubeImport(projectId: projectId, url: url);
      state = state.copyWith(
        phase: UploadPhase.completed,
        job: job,
        clearError: true,
      );
      await ref.read(projectsControllerProvider.notifier).load(refresh: true);
      await ref
          .read(sourcesControllerProvider.notifier)
          .watchProject(projectId, refresh: true);
    } on ApiException catch (error) {
      _fail(error.message);
    } on Object {
      _fail('Could not start that YouTube import.');
    }
  }

  void pause() {
    if (state.phase != UploadPhase.uploading) return;
    _cancellation?.pause();
    state = state.copyWith(phase: UploadPhase.paused);
  }

  Future<void> resume() async {
    final session = state.session;
    if (state.phase != UploadPhase.paused || session == null || _file == null) {
      return;
    }
    state = state.copyWith(phase: UploadPhase.uploading, clearError: true);
    try {
      final current = await _api.getUpload(session.uploadId);
      _completedParts.addEntries(
        current.uploadedParts.map((part) => MapEntry(part.partNumber, part)),
      );
      state = state.copyWith(session: current);
      await _continueUpload(current);
    } on ApiException catch (error) {
      _fail(error.message);
    } on UploadTransportException catch (error) {
      _fail(error.message);
    }
  }

  Future<void> retry() async {
    final sourceUrl = state.sourceUrl;
    final projectId = state.projectId;
    if (state.phase == UploadPhase.failed &&
        sourceUrl != null &&
        projectId != null) {
      state = const UploadState();
      await importYouTube(projectId, sourceUrl);
      return;
    }
    final session = state.session;
    if (state.phase != UploadPhase.failed || session == null || _file == null) {
      return;
    }
    state = state.copyWith(phase: UploadPhase.uploading, clearError: true);
    try {
      final current = await _api.getUpload(session.uploadId);
      _completedParts.addEntries(
        current.uploadedParts.map((part) => MapEntry(part.partNumber, part)),
      );
      state = state.copyWith(session: current);
      await _continueUpload(current);
    } on ApiException catch (error) {
      _fail(error.message);
    } on UploadTransportException catch (error) {
      _fail(error.message);
    }
  }

  bool _isYouTubeHost(String host) {
    final normalized = host.toLowerCase();
    return normalized == 'youtu.be' ||
        normalized == 'youtube.com' ||
        normalized.endsWith('.youtube.com') ||
        normalized == 'youtube-nocookie.com' ||
        normalized.endsWith('.youtube-nocookie.com');
  }

  Future<void> cancel() async {
    final session = state.session;
    _cancellation?.cancel();
    if (session == null) {
      _reset();
      return;
    }

    state = state.copyWith(phase: UploadPhase.cancelling, clearError: true);
    try {
      await _api.abort(session.uploadId);
      _reset();
    } on ApiException catch (error) {
      _fail(error.message);
    }
  }

  void clearCompleted() {
    if (state.phase == UploadPhase.completed) _reset();
  }

  Future<void> _continueUpload(UploadSession session) async {
    _cancellation = UploadCancellation();
    try {
      if (session.status != UploadStatus.completed) {
        if (session.mode == UploadMode.singlePut) {
          await _uploadSingle(session);
        } else {
          await _uploadMultipart(session);
        }
      }
      if (_cancellation!.isStopped) {
        throw UploadStoppedException(paused: _cancellation!.isPaused);
      }

      state = state.copyWith(phase: UploadPhase.completing);
      final parts = _completedParts.values.toList()
        ..sort((a, b) => a.partNumber.compareTo(b.partNumber));
      final job = await _api.complete(session.uploadId, parts);
      state = state.copyWith(
        phase: UploadPhase.completed,
        sentBytes: session.sizeBytes,
        job: job,
        clearError: true,
      );
      await ref.read(projectsControllerProvider.notifier).load(refresh: true);
      await ref
          .read(sourcesControllerProvider.notifier)
          .watchProject(session.projectId, refresh: true);
    } on UploadStoppedException catch (stopped) {
      if (stopped.paused) {
        state = state.copyWith(phase: UploadPhase.paused);
      } else if (state.phase != UploadPhase.cancelling) {
        _reset();
      }
    } on ApiException catch (error) {
      _fail(error.message);
    } on UploadTransportException catch (error) {
      _fail(error.message);
    }
  }

  Future<void> _uploadSingle(UploadSession session) async {
    var current = session;
    for (var attempt = 1; attempt <= _maxPartAttempts; attempt++) {
      final url = current.url;
      if (url == null) {
        throw const UploadTransportException('The upload URL is missing.');
      }
      try {
        await _uploads.put(
          UploadRequest(
            file: _file!,
            url: Uri.parse(url),
            start: 0,
            end: session.sizeBytes,
            contentType: session.contentType,
            cancellation: _cancellation!,
            onProgress: (sent, _) {
              state = state.copyWith(sentBytes: sent);
            },
          ),
        );
        return;
      } on UploadStoppedException {
        rethrow;
      } on UploadTransportException {
        if (attempt == _maxPartAttempts) rethrow;
        current = await _api.getUpload(session.uploadId);
        state = state.copyWith(session: current);
      }
    }
  }

  Future<void> _uploadMultipart(UploadSession session) async {
    final partSize = session.partSizeBytes;
    if (partSize == null) {
      throw const UploadTransportException('The multipart size is missing.');
    }
    final count = (session.sizeBytes + partSize - 1) ~/ partSize;

    while (_completedParts.length < count) {
      if (_cancellation!.isStopped) {
        throw UploadStoppedException(paused: _cancellation!.isPaused);
      }
      final pending = <int>[];
      for (
        var number = 1;
        number <= count && pending.length < _parallelParts;
        number++
      ) {
        if (!_completedParts.containsKey(number)) pending.add(number);
      }
      final urls = await _api.partUrls(session.uploadId, pending);
      await Future.wait(urls.map((url) => _uploadPart(session, url, partSize)));
    }
  }

  Future<void> _uploadPart(
    UploadSession session,
    PartUploadUrl initial,
    int partSize,
  ) async {
    var signed = initial;
    final number = signed.partNumber;
    final start = (number - 1) * partSize;
    final end = math.min(start + partSize, session.sizeBytes);

    for (var attempt = 1; attempt <= _maxPartAttempts; attempt++) {
      try {
        final etag = await _uploads.put(
          UploadRequest(
            file: _file!,
            url: Uri.parse(signed.url),
            start: start,
            end: end,
            contentType: session.contentType,
            cancellation: _cancellation!,
            onProgress: (sent, _) => _partProgress(number, sent),
          ),
        );
        if (etag == null || etag.isEmpty) {
          throw const UploadTransportException(
            'Object storage did not expose the multipart ETag.',
          );
        }
        _activePartBytes.remove(number);
        _completedParts[number] = UploadedPart(
          partNumber: number,
          etag: etag,
          sizeBytes: end - start,
        );
        _refreshProgress();
        return;
      } on UploadStoppedException {
        rethrow;
      } on UploadTransportException {
        _activePartBytes.remove(number);
        if (attempt == _maxPartAttempts) rethrow;
        signed = (await _api.partUrls(session.uploadId, [number])).single;
      }
    }
  }

  void _partProgress(int partNumber, int sent) {
    _activePartBytes[partNumber] = sent;
    _refreshProgress();
  }

  void _refreshProgress() {
    final complete = _completedParts.values.fold<int>(
      0,
      (sum, part) => sum + part.sizeBytes,
    );
    final active = _activePartBytes.values.fold<int>(
      0,
      (sum, bytes) => sum + bytes,
    );
    state = state.copyWith(sentBytes: complete + active);
  }

  String _contentType(XFile file) {
    final provided = file.mimeType;
    if (provided != null && provided.startsWith('video/')) return provided;
    final name = file.name.toLowerCase();
    if (name.endsWith('.mov')) return 'video/quicktime';
    if (name.endsWith('.webm')) return 'video/webm';
    if (name.endsWith('.mkv')) return 'video/x-matroska';
    if (name.endsWith('.mpeg') || name.endsWith('.mpg')) return 'video/mpeg';
    return 'video/mp4';
  }

  void _fail(String message) {
    _activePartBytes.clear();
    state = state.copyWith(phase: UploadPhase.failed, error: message);
  }

  void _reset() {
    _file = null;
    _cancellation = null;
    _completedParts.clear();
    _activePartBytes.clear();
    state = const UploadState();
  }
}
