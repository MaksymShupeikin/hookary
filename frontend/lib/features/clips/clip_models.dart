import '../import/import_models.dart';

class SuggestedClip {
  const SuggestedClip({
    required this.id,
    required this.projectId,
    required this.assetId,
    required this.title,
    required this.score,
    required this.startMs,
    required this.endMs,
    required this.status,
    this.renderAvailable,
    this.hook,
    this.reason,
    this.contextRisk,
  });

  factory SuggestedClip.fromJson(Map<String, dynamic> json) {
    return SuggestedClip(
      id: json['id'] as String,
      projectId: json['project_id'] as String,
      assetId: json['asset_id'] as String,
      title: json['title'] as String,
      hook: json['hook'] as String?,
      reason: json['reason'] as String?,
      score: (json['score'] as num).toDouble(),
      contextRisk: json['context_risk'] as String?,
      startMs: (json['start_ms'] as num).toInt(),
      endMs: (json['end_ms'] as num).toInt(),
      status: json['status'] as String,
      renderAvailable: json['can_render'] as bool?,
    );
  }

  final String id;
  final String projectId;
  final String assetId;
  final String title;
  final String? hook;
  final String? reason;
  final double score;
  final String? contextRisk;
  final int startMs;
  final int endMs;
  final String status;
  final bool? renderAvailable;

  int get durationMs => endMs - startMs;
  bool get canRender =>
      renderAvailable ?? status == 'candidate' || status == 'failed';
  bool get isRendering => status == 'rendering';
  bool get isRendered => status == 'rendered';
}

class ClipDownload {
  const ClipDownload({
    required this.url,
    required this.filename,
    required this.expiresAt,
  });

  factory ClipDownload.fromJson(Map<String, dynamic> json) {
    return ClipDownload(
      url: json['url'] as String,
      filename: json['filename'] as String,
      expiresAt: DateTime.parse(json['expires_at'] as String),
    );
  }

  final String url;
  final String filename;
  final DateTime expiresAt;
}

class RenderedClipPreview {
  const RenderedClipPreview({required this.url, required this.expiresAt});

  factory RenderedClipPreview.fromJson(Map<String, dynamic> json) {
    return RenderedClipPreview(
      url: json['url'] as String,
      expiresAt: DateTime.parse(json['expires_at'] as String),
    );
  }

  final String url;
  final DateTime expiresAt;
}

class ClipsState {
  const ClipsState({
    this.projectId,
    this.clips = const [],
    this.isLoading = false,
    this.hasLoaded = false,
    this.previewClipId,
    this.previewUrl,
    this.previewIsRendered = false,
    this.transcript,
    this.previewLoading = false,
    this.transcriptLoading = false,
    this.selectedClipIds = const {},
    this.renderLoading = false,
    this.downloadingClipId,
    this.error,
    this.actionError,
    this.previewError,
    this.transcriptError,
  });

  final String? projectId;
  final List<SuggestedClip> clips;
  final bool isLoading;
  final bool hasLoaded;
  final String? previewClipId;
  final String? previewUrl;
  final bool previewIsRendered;
  final TranscriptDocument? transcript;
  final bool previewLoading;
  final bool transcriptLoading;
  final Set<String> selectedClipIds;
  final bool renderLoading;
  final String? downloadingClipId;
  final String? error;
  final String? actionError;
  final String? previewError;
  final String? transcriptError;

  ClipsState copyWith({
    List<SuggestedClip>? clips,
    bool? isLoading,
    bool? hasLoaded,
    String? previewClipId,
    String? previewUrl,
    bool? previewIsRendered,
    TranscriptDocument? transcript,
    bool? previewLoading,
    bool? transcriptLoading,
    Set<String>? selectedClipIds,
    bool? renderLoading,
    String? downloadingClipId,
    String? error,
    String? actionError,
    String? previewError,
    String? transcriptError,
    bool clearError = false,
    bool clearPreview = false,
    bool clearPreviewUrl = false,
    bool clearTranscript = false,
    bool clearPreviewError = false,
    bool clearTranscriptError = false,
    bool clearDownload = false,
    bool clearActionError = false,
  }) {
    return ClipsState(
      projectId: projectId,
      clips: clips ?? this.clips,
      isLoading: isLoading ?? this.isLoading,
      hasLoaded: hasLoaded ?? this.hasLoaded,
      previewClipId: clearPreview ? null : previewClipId ?? this.previewClipId,
      previewUrl: clearPreview || clearPreviewUrl
          ? null
          : previewUrl ?? this.previewUrl,
      previewIsRendered: clearPreview || clearPreviewUrl
          ? false
          : previewIsRendered ?? this.previewIsRendered,
      transcript: clearPreview || clearTranscript
          ? null
          : transcript ?? this.transcript,
      previewLoading: clearPreview
          ? false
          : previewLoading ?? this.previewLoading,
      transcriptLoading: clearPreview
          ? false
          : transcriptLoading ?? this.transcriptLoading,
      selectedClipIds: selectedClipIds ?? this.selectedClipIds,
      renderLoading: renderLoading ?? this.renderLoading,
      downloadingClipId: clearDownload
          ? null
          : downloadingClipId ?? this.downloadingClipId,
      error: clearError ? null : error ?? this.error,
      actionError: clearActionError ? null : actionError ?? this.actionError,
      previewError: clearPreview || clearPreviewError
          ? null
          : previewError ?? this.previewError,
      transcriptError: clearPreview || clearTranscriptError
          ? null
          : transcriptError ?? this.transcriptError,
    );
  }
}
