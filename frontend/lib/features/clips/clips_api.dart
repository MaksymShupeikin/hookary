import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/services/api_client.dart';
import '../auth/auth_controller.dart';
import 'clip_models.dart';

final clipsApiProvider = Provider<ClipsApi>((ref) {
  return ClipsApi(ref.watch(apiClientProvider));
});

class ClipsApi {
  ClipsApi(this._client);

  final ApiClient _client;

  Future<List<SuggestedClip>> listProjectClips(String projectId) async {
    final json = await _client.get<Map<String, dynamic>>(
      '/v1/projects/$projectId/clips',
    );
    final clips = json['clips'] as List<dynamic>? ?? const [];
    return clips
        .map((item) => SuggestedClip.fromJson(item as Map<String, dynamic>))
        .toList(growable: false);
  }

  Future<void> renderClips(String projectId, Set<String> clipIds) async {
    await _client.post<Map<String, dynamic>>(
      '/v1/projects/$projectId/clips/render',
      body: {'clip_ids': clipIds.toList(growable: false)},
    );
  }

  Future<ClipDownload> downloadUrl(String clipId) async {
    final json = await _client.post<Map<String, dynamic>>(
      '/v1/clips/$clipId/download-url',
    );
    return ClipDownload.fromJson(json);
  }

  Future<RenderedClipPreview> renderedPreviewUrl(String clipId) async {
    final json = await _client.post<Map<String, dynamic>>(
      '/v1/clips/$clipId/preview-url',
    );
    return RenderedClipPreview.fromJson(json);
  }
}
