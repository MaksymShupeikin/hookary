import 'package:flutter_test/flutter_test.dart';
import 'package:hookary/features/clips/clip_models.dart';

void main() {
  test('maps a structured clip suggestion and derives its duration', () {
    final clip = SuggestedClip.fromJson({
      'id': '01JZ8XKQ4T6N9R2VBC3D5EFGHJ',
      'project_id': '01JZ8WKQ4T6N9R2VBC3D5EFGHJ',
      'asset_id': '01JZ8YKQ4T6N9R2VBC3D5EFGHJ',
      'title': 'A strong standalone idea',
      'hook': 'Most creators miss this.',
      'reason': 'Clear hook and complete payoff.',
      'score': 0.91,
      'context_risk': 'low',
      'start_ms': 12000,
      'end_ms': 49000,
      'status': 'candidate',
      'can_render': true,
    });

    expect(clip.score, 0.91);
    expect(clip.durationMs, 37000);
    expect(clip.contextRisk, 'low');
    expect(clip.canRender, isTrue);
  });

  test('allows an older rendered clip to be upgraded once', () {
    final clip = SuggestedClip.fromJson({
      'id': '01JZ8XKQ4T6N9R2VBC3D5EFGHJ',
      'project_id': '01JZ8WKQ4T6N9R2VBC3D5EFGHJ',
      'asset_id': '01JZ8YKQ4T6N9R2VBC3D5EFGHJ',
      'title': 'Older render',
      'score': 0.91,
      'start_ms': 12000,
      'end_ms': 49000,
      'status': 'rendered',
      'can_render': true,
    });

    expect(clip.isRendered, isTrue);
    expect(clip.canRender, isTrue);
  });

  test('maps a short-lived rendered clip download', () {
    final download = ClipDownload.fromJson({
      'url': 'https://storage.example/output.mp4?signature=short-lived',
      'filename': 'hookary-clip.mp4',
      'expires_at': '2026-07-17T12:15:00Z',
    });

    expect(download.filename, 'hookary-clip.mp4');
    expect(download.expiresAt, DateTime.utc(2026, 7, 17, 12, 15));
  });

  test('maps a short-lived rendered clip preview', () {
    final preview = RenderedClipPreview.fromJson({
      'url': 'https://storage.example/output.mp4?signature=inline',
      'expires_at': '2026-07-17T12:15:00Z',
    });

    expect(preview.url, contains('signature=inline'));
  });
}
