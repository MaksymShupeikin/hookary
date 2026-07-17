import 'package:flutter_test/flutter_test.dart';
import 'package:hookary/core/services/upload_client_types.dart';
import 'package:hookary/features/import/import_models.dart';

void main() {
  test('single PUT JSON maps to the Dart enum without leaking wire names', () {
    final session = UploadSession.fromJson({
      'upload_id': '01JZ8XKQ4T6N9R2VBC3D5EFGHJ',
      'asset_id': '01JZ8XKQ4T6N9R2VBC3D5EFGHK',
      'project_id': '01JZ8XKQ4T6N9R2VBC3D5EFGHM',
      'mode': 'single_put',
      'url': 'http://localhost:9000/upload',
      'part_size_bytes': null,
      'size_bytes': 100,
      'content_type': 'video/mp4',
      'status': 'pending',
      'uploaded_parts': <Object>[],
      'expires_at': '2026-07-16T20:00:00Z',
    });

    expect(session.mode, UploadMode.singlePut);
    expect(session.status, UploadStatus.pending);
    expect(session.url, startsWith('http://localhost:9000'));
  });

  test('upload progress is bounded by the declared file size', () {
    const state = UploadState(sizeBytes: 100, sentBytes: 125);
    expect(state.progress, 1);
  });

  test('project source JSON keeps preview readiness separate from status', () {
    final source = SourceAsset.fromJson({
      'id': '01JZ8XKQ4T6N9R2VBC3D5EFGHJ',
      'project_id': '01JZ8WKQ4T6N9R2VBC3D5EFGHJ',
      'source_kind': 'public_url',
      'title': 'Downloaded YouTube source',
      'status': 'ready',
      'preview_ready': true,
      'transcript_ready': true,
      'transcript_status': 'succeeded',
      'analysis_status': 'succeeded',
      'content_type': 'video/mp4',
      'size_bytes': 2048,
      'duration_ms': 19000,
      'width': 1280,
      'height': 720,
      'created_at': '2026-07-17T12:00:00Z',
      'updated_at': '2026-07-17T12:01:00Z',
    });

    expect(source.kind, SourceKind.publicUrl);
    expect(source.status, SourceStatus.ready);
    expect(source.previewReady, isTrue);
    expect(source.transcriptReady, isTrue);
    expect(source.title, 'Downloaded YouTube source');
  });

  test('timed transcript keeps compact readable cues', () {
    final transcript = TranscriptDocument.fromJson({
      'source': 'youtube_manual',
      'language': 'en',
      'duration_ms': 9000,
      'cues': [
        {'start_ms': 1200, 'end_ms': 3400, 'text': 'A useful opening.'},
      ],
    });

    expect(transcript.sourceLabel, 'YouTube captions');
    expect(transcript.cues.single.startMs, 1200);
    expect(transcript.cues.single.text, 'A useful opening.');
  });

  test('pause aborts active requests and cancel wins permanently', () {
    final cancellation = UploadCancellation();
    var aborted = 0;
    cancellation.attach(() => aborted++);

    cancellation.pause();
    expect(aborted, 1);
    expect(cancellation.isPaused, isTrue);

    cancellation.attach(() => aborted++);
    expect(aborted, 2);

    cancellation.cancel();
    expect(cancellation.isCancelled, isTrue);
    expect(cancellation.isPaused, isFalse);
  });
}
