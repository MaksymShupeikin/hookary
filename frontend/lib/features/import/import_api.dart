import 'dart:convert';

import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/services/api_client.dart';
import '../auth/auth_controller.dart';
import 'import_models.dart';

final importApiProvider = Provider<ImportApi>((ref) {
  return ImportApi(ref.watch(apiClientProvider));
});

class ImportApi {
  ImportApi(this._client) : _storageClient = Dio();

  final ApiClient _client;
  final Dio _storageClient;

  Future<UploadSession> startUpload({
    required String projectId,
    required String filename,
    required String contentType,
    required int sizeBytes,
  }) async {
    final json = await _client.post<Map<String, dynamic>>(
      '/v1/uploads',
      body: {
        'project_id': projectId,
        'filename': filename,
        'content_type': contentType,
        'size_bytes': sizeBytes,
      },
    );
    return UploadSession.fromJson(json);
  }

  Future<QueuedJob> startYouTubeImport({
    required String projectId,
    required String url,
  }) async {
    final json = await _client.post<Map<String, dynamic>>(
      '/v1/imports',
      body: {'project_id': projectId, 'url': url},
    );
    return QueuedJob.fromJson(json);
  }

  Future<UploadSession> getUpload(String uploadId) async {
    final json = await _client.get<Map<String, dynamic>>(
      '/v1/uploads/$uploadId',
    );
    return UploadSession.fromJson(json);
  }

  Future<List<PartUploadUrl>> partUrls(
    String uploadId,
    List<int> partNumbers,
  ) async {
    final json = await _client.post<Map<String, dynamic>>(
      '/v1/uploads/$uploadId/parts',
      body: {'part_numbers': partNumbers},
    );
    final items = json['parts'] as List<dynamic>? ?? const [];
    return items
        .map((item) => PartUploadUrl.fromJson(item as Map<String, dynamic>))
        .toList(growable: false);
  }

  Future<QueuedJob> complete(String uploadId, List<UploadedPart> parts) async {
    final json = await _client.post<Map<String, dynamic>>(
      '/v1/uploads/$uploadId/complete',
      body: {'parts': parts.map((part) => part.toCompleteJson()).toList()},
    );
    return QueuedJob.fromJson(json);
  }

  Future<void> abort(String uploadId) =>
      _client.delete<void>('/v1/uploads/$uploadId');

  Future<List<SourceAsset>> listSources(String projectId) async {
    final json = await _client.get<Map<String, dynamic>>(
      '/v1/projects/$projectId/assets',
    );
    final assets = json['assets'] as List<dynamic>? ?? const [];
    return assets
        .map((item) => SourceAsset.fromJson(item as Map<String, dynamic>))
        .toList(growable: false);
  }

  Future<PreviewUrls> previewUrls(String assetId) async {
    final json = await _client.post<Map<String, dynamic>>(
      '/v1/assets/$assetId/preview-url',
    );
    return PreviewUrls.fromJson(json);
  }

  Future<QueuedJob> startAnalysis(String assetId) async {
    final json = await _client.post<Map<String, dynamic>>(
      '/v1/assets/$assetId/analyze',
    );
    return QueuedJob.fromJson(json);
  }

  Future<TranscriptDocument> loadTranscript(String url) async {
    final response = await _storageClient.get<dynamic>(
      url,
      options: Options(responseType: ResponseType.json),
    );
    final data = response.data;
    if (data is Map<String, dynamic>) {
      return TranscriptDocument.fromJson(data);
    }
    if (data is String) {
      return TranscriptDocument.fromJson(
        jsonDecode(data) as Map<String, dynamic>,
      );
    }
    throw const FormatException('Invalid transcript response.');
  }
}
