import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/exceptions/exceptions.dart';
import '../../core/services/download_client.dart';
import '../import/import_api.dart';
import 'clip_models.dart';
import 'clips_api.dart';

final clipsControllerProvider = NotifierProvider<ClipsController, ClipsState>(
  ClipsController.new,
);

final downloadClientProvider = Provider<DownloadClient>((ref) {
  return const DownloadClient();
});

class ClipsController extends Notifier<ClipsState> {
  var _generation = 0;
  Timer? _pollTimer;

  @override
  ClipsState build() {
    ref.onDispose(() {
      _generation++;
      _pollTimer?.cancel();
    });
    return const ClipsState();
  }

  ClipsApi get _api => ref.read(clipsApiProvider);
  ImportApi get _previewApi => ref.read(importApiProvider);
  DownloadClient get _downloads => ref.read(downloadClientProvider);

  Future<void> watchProject(String projectId) async {
    if (state.projectId == projectId && state.hasLoaded) return;
    if (state.projectId != projectId) {
      _generation++;
      _pollTimer?.cancel();
      _pollTimer = null;
      state = ClipsState(projectId: projectId, isLoading: true);
    }
    await _load(projectId, _generation);
  }

  Future<void> refresh() async {
    final projectId = state.projectId;
    if (projectId == null) return;
    state = state.copyWith(isLoading: true, clearError: true);
    await _load(projectId, _generation);
  }

  Future<void> openPreview(SuggestedClip clip) async {
    if (state.previewLoading) return;
    if (state.previewClipId == clip.id) {
      closePreview();
      return;
    }
    final projectId = state.projectId;
    if (projectId == null || clip.projectId != projectId) return;
    final generation = _generation;
    state = state.copyWith(
      previewClipId: clip.id,
      previewLoading: true,
      transcriptLoading: false,
      clearPreviewUrl: true,
      clearTranscript: true,
      clearPreviewError: true,
      clearTranscriptError: true,
    );
    try {
      if (clip.isRendered) {
        final preview = await _api.renderedPreviewUrl(clip.id);
        if (!_previewIsCurrent(projectId, clip.id, generation)) return;
        state = state.copyWith(
          previewUrl: preview.url,
          previewIsRendered: true,
          previewLoading: false,
        );
        return;
      }
      final urls = await _previewApi.previewUrls(clip.assetId);
      if (!_previewIsCurrent(projectId, clip.id, generation)) return;
      state = state.copyWith(
        previewUrl: urls.videoUrl,
        previewLoading: false,
        transcriptLoading: urls.subtitlesUrl != null,
      );
      final subtitlesUrl = urls.subtitlesUrl;
      if (subtitlesUrl != null) {
        await _loadTranscript(projectId, clip.id, subtitlesUrl, generation);
      }
    } on ApiException catch (error) {
      if (!_previewIsCurrent(projectId, clip.id, generation)) return;
      state = state.copyWith(
        previewLoading: false,
        previewError: error.message,
      );
    }
  }

  void closePreview() {
    state = state.copyWith(clearPreview: true);
  }

  void toggleSelection(SuggestedClip clip) {
    if (!clip.canRender || state.renderLoading) return;
    final selected = {...state.selectedClipIds};
    if (!selected.remove(clip.id)) {
      selected.add(clip.id);
    }
    state = state.copyWith(selectedClipIds: selected, clearActionError: true);
  }

  void selectAllRenderable() {
    if (state.renderLoading) return;
    final renderable = state.clips
        .where((clip) => clip.canRender)
        .map((clip) => clip.id)
        .toSet();
    final allSelected =
        renderable.isNotEmpty &&
        renderable.every(state.selectedClipIds.contains);
    state = state.copyWith(
      selectedClipIds: allSelected ? <String>{} : renderable,
      clearActionError: true,
    );
  }

  Future<void> renderSelected() async {
    final projectId = state.projectId;
    final selected = {...state.selectedClipIds};
    if (projectId == null || selected.isEmpty || state.renderLoading) return;
    state = state.copyWith(renderLoading: true, clearActionError: true);
    try {
      await _api.renderClips(projectId, selected);
      state = state.copyWith(
        selectedClipIds: const {},
        renderLoading: false,
        clearActionError: true,
      );
      await _load(projectId, _generation);
    } on ApiException catch (error) {
      state = state.copyWith(renderLoading: false, actionError: error.message);
    }
  }

  Future<void> download(SuggestedClip clip) async {
    if (!clip.isRendered || state.downloadingClipId != null) return;
    state = state.copyWith(downloadingClipId: clip.id, clearActionError: true);
    try {
      final download = await _api.downloadUrl(clip.id);
      await _downloads.save(url: download.url, filename: download.filename);
      state = state.copyWith(clearDownload: true, clearActionError: true);
    } on ApiException catch (error) {
      state = state.copyWith(clearDownload: true, actionError: error.message);
    } catch (_) {
      state = state.copyWith(
        clearDownload: true,
        actionError: 'The device could not start the video download.',
      );
    }
  }

  Future<void> _load(String projectId, int generation) async {
    try {
      final clips = await _api.listProjectClips(projectId);
      if (generation != _generation || state.projectId != projectId) return;
      final previewStillExists =
          state.previewClipId == null ||
          clips.any((clip) => clip.id == state.previewClipId);
      final renderableIDs = clips
          .where((clip) => clip.canRender)
          .map((clip) => clip.id)
          .toSet();
      state = state.copyWith(
        clips: clips,
        selectedClipIds: state.selectedClipIds.intersection(renderableIDs),
        isLoading: false,
        hasLoaded: true,
        clearError: true,
        clearPreview: !previewStillExists,
      );
      _updatePolling(projectId, generation);
    } on ApiException catch (error) {
      if (generation != _generation || state.projectId != projectId) return;
      state = state.copyWith(
        isLoading: false,
        hasLoaded: true,
        error: error.message,
      );
      _updatePolling(projectId, generation);
    }
  }

  Future<void> _loadTranscript(
    String projectId,
    String clipId,
    String url,
    int generation,
  ) async {
    try {
      final transcript = await _previewApi.loadTranscript(url);
      if (!_previewIsCurrent(projectId, clipId, generation)) return;
      state = state.copyWith(
        transcript: transcript,
        transcriptLoading: false,
        clearTranscriptError: true,
      );
    } catch (_) {
      if (!_previewIsCurrent(projectId, clipId, generation)) return;
      state = state.copyWith(
        transcriptLoading: false,
        transcriptError: 'Subtitles could not be loaded for this preview.',
      );
    }
  }

  bool _previewIsCurrent(String projectId, String clipId, int generation) {
    return generation == _generation &&
        state.projectId == projectId &&
        state.previewClipId == clipId;
  }

  void _updatePolling(String projectId, int generation) {
    if (!state.clips.any((clip) => clip.isRendering)) {
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
}
