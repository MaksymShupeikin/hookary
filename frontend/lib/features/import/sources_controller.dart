import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/exceptions/exceptions.dart';
import 'import_api.dart';
import 'import_models.dart';

final sourcesControllerProvider =
    NotifierProvider<SourcesController, SourcesState>(SourcesController.new);

class SourcesController extends Notifier<SourcesState> {
  Timer? _pollTimer;
  var _generation = 0;

  @override
  SourcesState build() {
    ref.onDispose(() {
      _generation++;
      _pollTimer?.cancel();
    });
    return const SourcesState();
  }

  ImportApi get _api => ref.read(importApiProvider);

  Future<void> watchProject(String projectId, {bool refresh = false}) async {
    if (!refresh && state.projectId == projectId && state.hasLoaded) return;
    if (state.projectId != projectId) {
      _generation++;
      _pollTimer?.cancel();
      state = SourcesState(projectId: projectId, isLoading: true);
    } else if (!state.hasLoaded) {
      state = state.copyWith(isLoading: true, clearError: true);
    }
    await _load(projectId, _generation);
  }

  Future<void> refresh() async {
    final projectId = state.projectId;
    if (projectId == null) return;
    await _load(projectId, _generation);
  }

  Future<void> openPreview(SourceAsset asset) async {
    if (!asset.previewReady || state.previewLoading) return;
    state = state.copyWith(
      selectedAssetId: asset.id,
      previewLoading: true,
      clearPreviewUrl: true,
      clearTranscript: true,
      clearPreviewError: true,
      clearTranscriptError: true,
    );
    try {
      final urls = await _api.previewUrls(asset.id);
      if (state.selectedAssetId != asset.id) return;
      state = state.copyWith(
        previewUrl: urls.videoUrl,
        previewLoading: false,
        transcriptLoading: urls.subtitlesUrl != null,
      );
      final subtitlesUrl = urls.subtitlesUrl;
      if (subtitlesUrl != null) {
        await _loadTranscript(asset.id, subtitlesUrl);
      }
    } on ApiException catch (error) {
      if (state.selectedAssetId != asset.id) return;
      state = state.copyWith(
        previewLoading: false,
        previewError: error.message,
      );
    }
  }

  void closePreview() {
    state = state.copyWith(
      clearSelection: true,
      previewLoading: false,
      transcriptLoading: false,
    );
  }

  Future<void> startAnalysis(SourceAsset asset) async {
    if (asset.status != SourceStatus.ready || asset.transcriptReady) return;
    try {
      await _api.startAnalysis(asset.id);
      final projectId = state.projectId;
      if (projectId != null) {
        await _load(projectId, _generation);
      }
    } on ApiException catch (error) {
      state = state.copyWith(error: error.message);
    }
  }

  Future<void> _load(String projectId, int generation) async {
    try {
      final assets = await _api.listSources(projectId);
      if (generation != _generation || state.projectId != projectId) return;
      state = state.copyWith(
        assets: assets,
        isLoading: false,
        hasLoaded: true,
        clearError: true,
      );
      _updatePolling(projectId, generation);
      _refreshSelectedTranscript(assets);
    } on ApiException catch (error) {
      if (generation != _generation || state.projectId != projectId) return;
      state = state.copyWith(
        isLoading: false,
        hasLoaded: true,
        error: error.message,
      );
      if (error.statusCode == 404) {
        _pollTimer?.cancel();
        _pollTimer = null;
        return;
      }
      _updatePolling(projectId, generation);
    }
  }

  void _updatePolling(String projectId, int generation) {
    if (!state.hasPending) {
      _pollTimer?.cancel();
      _pollTimer = null;
      return;
    }
    if (_pollTimer != null) return;
    _pollTimer = Timer.periodic(const Duration(seconds: 4), (_) {
      if (generation == _generation && state.projectId == projectId) {
        unawaited(_load(projectId, generation));
      }
    });
  }

  Future<void> _loadTranscript(String assetId, String url) async {
    try {
      final transcript = await _api.loadTranscript(url);
      if (state.selectedAssetId != assetId) return;
      state = state.copyWith(
        transcript: transcript,
        transcriptLoading: false,
        clearTranscriptError: true,
      );
    } catch (_) {
      if (state.selectedAssetId != assetId) return;
      state = state.copyWith(
        transcriptLoading: false,
        transcriptError:
            'The subtitles could not be loaded. Try reopening the preview.',
      );
    }
  }

  void _refreshSelectedTranscript(List<SourceAsset> assets) {
    final selectedAssetId = state.selectedAssetId;
    if (selectedAssetId == null ||
        state.previewUrl == null ||
        state.transcript != null ||
        state.transcriptLoading ||
        state.transcriptError != null) {
      return;
    }
    SourceAsset? selected;
    for (final asset in assets) {
      if (asset.id == selectedAssetId) {
        selected = asset;
        break;
      }
    }
    if (selected?.transcriptReady != true) return;
    state = state.copyWith(transcriptLoading: true);
    unawaited(_reloadSelectedTranscript(selectedAssetId));
  }

  Future<void> _reloadSelectedTranscript(String assetId) async {
    try {
      final urls = await _api.previewUrls(assetId);
      if (state.selectedAssetId != assetId) return;
      if (urls.subtitlesUrl == null) {
        state = state.copyWith(
          transcriptLoading: false,
          transcriptError: 'The transcript is ready but has no preview URL.',
        );
        return;
      }
      await _loadTranscript(assetId, urls.subtitlesUrl!);
    } on ApiException catch (error) {
      if (state.selectedAssetId != assetId) return;
      state = state.copyWith(
        transcriptLoading: false,
        transcriptError: error.message,
      );
    }
  }
}
