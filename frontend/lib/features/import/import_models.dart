enum UploadMode { singlePut, multipart }

enum UploadStatus { pending, completed, aborted }

enum SourceKind { r2Upload, publicUrl }

enum SourceStatus { pending, ready, failed }

enum TranscriptStatus { pending, queued, running, succeeded, failed }

class SourceAsset {
  const SourceAsset({
    required this.id,
    required this.projectId,
    required this.kind,
    required this.title,
    required this.status,
    required this.previewReady,
    required this.transcriptReady,
    required this.transcriptStatus,
    required this.analysisStatus,
    required this.createdAt,
    required this.updatedAt,
    this.contentType,
    this.sizeBytes,
    this.durationMs,
    this.width,
    this.height,
  });

  factory SourceAsset.fromJson(Map<String, dynamic> json) {
    return SourceAsset(
      id: json['id'] as String,
      projectId: json['project_id'] as String,
      kind: switch (json['source_kind']) {
        'r2_upload' => SourceKind.r2Upload,
        'public_url' => SourceKind.publicUrl,
        final value => throw FormatException('Unknown source kind: $value'),
      },
      title: json['title'] as String,
      status: SourceStatus.values.byName(json['status'] as String),
      previewReady: json['preview_ready'] as bool,
      transcriptReady: json['transcript_ready'] as bool,
      transcriptStatus: TranscriptStatus.values.byName(
        json['transcript_status'] as String,
      ),
      analysisStatus: TranscriptStatus.values.byName(
        json['analysis_status'] as String,
      ),
      contentType: json['content_type'] as String?,
      sizeBytes: (json['size_bytes'] as num?)?.toInt(),
      durationMs: (json['duration_ms'] as num?)?.toInt(),
      width: (json['width'] as num?)?.toInt(),
      height: (json['height'] as num?)?.toInt(),
      createdAt: DateTime.parse(json['created_at'] as String),
      updatedAt: DateTime.parse(json['updated_at'] as String),
    );
  }

  final String id;
  final String projectId;
  final SourceKind kind;
  final String title;
  final SourceStatus status;
  final bool previewReady;
  final bool transcriptReady;
  final TranscriptStatus transcriptStatus;
  final TranscriptStatus analysisStatus;
  final String? contentType;
  final int? sizeBytes;
  final int? durationMs;
  final int? width;
  final int? height;
  final DateTime createdAt;
  final DateTime updatedAt;
}

class PreviewUrls {
  const PreviewUrls({required this.videoUrl, this.subtitlesUrl});

  factory PreviewUrls.fromJson(Map<String, dynamic> json) {
    return PreviewUrls(
      videoUrl: json['url'] as String,
      subtitlesUrl: json['subtitles_url'] as String?,
    );
  }

  final String videoUrl;
  final String? subtitlesUrl;
}

class TranscriptDocument {
  const TranscriptDocument({
    required this.source,
    required this.language,
    required this.durationMs,
    required this.cues,
  });

  factory TranscriptDocument.fromJson(Map<String, dynamic> json) {
    final cues = json['cues'] as List<dynamic>? ?? const [];
    return TranscriptDocument(
      source: json['source'] as String,
      language: json['language'] as String? ?? '',
      durationMs: (json['duration_ms'] as num).toInt(),
      cues: cues
          .map((item) => TranscriptCue.fromJson(item as Map<String, dynamic>))
          .toList(growable: false),
    );
  }

  final String source;
  final String language;
  final int durationMs;
  final List<TranscriptCue> cues;

  String get sourceLabel => switch (source) {
    'youtube_manual' => 'YouTube captions',
    'youtube_auto' => 'YouTube auto-captions',
    'whisper-1' => 'Whisper transcript',
    _ => 'Transcript',
  };
}

class TranscriptCue {
  const TranscriptCue({
    required this.startMs,
    required this.endMs,
    required this.text,
  });

  factory TranscriptCue.fromJson(Map<String, dynamic> json) {
    return TranscriptCue(
      startMs: (json['start_ms'] as num).toInt(),
      endMs: (json['end_ms'] as num).toInt(),
      text: json['text'] as String,
    );
  }

  final int startMs;
  final int endMs;
  final String text;
}

class SourcesState {
  const SourcesState({
    this.projectId,
    this.assets = const [],
    this.isLoading = false,
    this.hasLoaded = false,
    this.selectedAssetId,
    this.previewUrl,
    this.transcript,
    this.previewLoading = false,
    this.transcriptLoading = false,
    this.error,
    this.previewError,
    this.transcriptError,
  });

  final String? projectId;
  final List<SourceAsset> assets;
  final bool isLoading;
  final bool hasLoaded;
  final String? selectedAssetId;
  final String? previewUrl;
  final TranscriptDocument? transcript;
  final bool previewLoading;
  final bool transcriptLoading;
  final String? error;
  final String? previewError;
  final String? transcriptError;

  bool get hasPending => assets.any(
    (asset) =>
        asset.status == SourceStatus.pending ||
        asset.transcriptStatus == TranscriptStatus.pending ||
        asset.transcriptStatus == TranscriptStatus.queued ||
        asset.transcriptStatus == TranscriptStatus.running ||
        asset.analysisStatus == TranscriptStatus.pending ||
        asset.analysisStatus == TranscriptStatus.queued ||
        asset.analysisStatus == TranscriptStatus.running,
  );

  SourcesState copyWith({
    List<SourceAsset>? assets,
    bool? isLoading,
    bool? hasLoaded,
    String? selectedAssetId,
    String? previewUrl,
    TranscriptDocument? transcript,
    bool? previewLoading,
    bool? transcriptLoading,
    String? error,
    String? previewError,
    String? transcriptError,
    bool clearSelection = false,
    bool clearPreviewUrl = false,
    bool clearTranscript = false,
    bool clearError = false,
    bool clearPreviewError = false,
    bool clearTranscriptError = false,
  }) {
    return SourcesState(
      projectId: projectId,
      assets: assets ?? this.assets,
      isLoading: isLoading ?? this.isLoading,
      hasLoaded: hasLoaded ?? this.hasLoaded,
      selectedAssetId: clearSelection
          ? null
          : selectedAssetId ?? this.selectedAssetId,
      previewUrl: clearSelection || clearPreviewUrl
          ? null
          : previewUrl ?? this.previewUrl,
      transcript: clearSelection || clearTranscript
          ? null
          : transcript ?? this.transcript,
      previewLoading: previewLoading ?? this.previewLoading,
      transcriptLoading: transcriptLoading ?? this.transcriptLoading,
      error: clearError ? null : error ?? this.error,
      previewError: clearPreviewError
          ? null
          : previewError ?? this.previewError,
      transcriptError: clearTranscriptError
          ? null
          : transcriptError ?? this.transcriptError,
    );
  }
}

class UploadedPart {
  const UploadedPart({
    required this.partNumber,
    required this.etag,
    required this.sizeBytes,
  });

  factory UploadedPart.fromJson(Map<String, dynamic> json) {
    return UploadedPart(
      partNumber: json['part_number'] as int,
      etag: json['etag'] as String,
      sizeBytes: json['size_bytes'] as int,
    );
  }

  final int partNumber;
  final String etag;
  final int sizeBytes;

  Map<String, dynamic> toCompleteJson() => {
    'part_number': partNumber,
    'etag': etag,
  };
}

class UploadSession {
  const UploadSession({
    required this.uploadId,
    required this.assetId,
    required this.projectId,
    required this.mode,
    required this.sizeBytes,
    required this.contentType,
    required this.status,
    required this.uploadedParts,
    required this.expiresAt,
    this.url,
    this.partSizeBytes,
  });

  factory UploadSession.fromJson(Map<String, dynamic> json) {
    final parts = json['uploaded_parts'] as List<dynamic>? ?? const [];
    return UploadSession(
      uploadId: json['upload_id'] as String,
      assetId: json['asset_id'] as String,
      projectId: json['project_id'] as String,
      mode: switch (json['mode']) {
        'single_put' => UploadMode.singlePut,
        'multipart' => UploadMode.multipart,
        final value => throw FormatException('Unknown upload mode: $value'),
      },
      url: json['url'] as String?,
      partSizeBytes: json['part_size_bytes'] as int?,
      sizeBytes: json['size_bytes'] as int,
      contentType: json['content_type'] as String,
      status: UploadStatus.values.byName(json['status'] as String),
      uploadedParts: parts
          .map((part) => UploadedPart.fromJson(part as Map<String, dynamic>))
          .toList(growable: false),
      expiresAt: DateTime.parse(json['expires_at'] as String),
    );
  }

  final String uploadId;
  final String assetId;
  final String projectId;
  final UploadMode mode;
  final String? url;
  final int? partSizeBytes;
  final int sizeBytes;
  final String contentType;
  final UploadStatus status;
  final List<UploadedPart> uploadedParts;
  final DateTime expiresAt;
}

class PartUploadUrl {
  const PartUploadUrl({
    required this.partNumber,
    required this.url,
    required this.expiresAt,
  });

  factory PartUploadUrl.fromJson(Map<String, dynamic> json) {
    return PartUploadUrl(
      partNumber: json['part_number'] as int,
      url: json['url'] as String,
      expiresAt: DateTime.parse(json['expires_at'] as String),
    );
  }

  final int partNumber;
  final String url;
  final DateTime expiresAt;
}

class QueuedJob {
  const QueuedJob({
    required this.id,
    required this.projectId,
    required this.stage,
    required this.status,
  });

  factory QueuedJob.fromJson(Map<String, dynamic> json) {
    return QueuedJob(
      id: json['id'] as String,
      projectId: json['project_id'] as String,
      stage: json['stage'] as String,
      status: json['status'] as String,
    );
  }

  final String id;
  final String projectId;
  final String stage;
  final String status;
}

enum UploadPhase {
  idle,
  choosing,
  importingUrl,
  uploading,
  paused,
  completing,
  completed,
  cancelling,
  failed,
}

class UploadState {
  const UploadState({
    this.phase = UploadPhase.idle,
    this.projectId,
    this.sourceUrl,
    this.filename,
    this.sizeBytes,
    this.sentBytes = 0,
    this.session,
    this.job,
    this.error,
  });

  final UploadPhase phase;
  final String? projectId;
  final String? sourceUrl;
  final String? filename;
  final int? sizeBytes;
  final int sentBytes;
  final UploadSession? session;
  final QueuedJob? job;
  final String? error;

  double get progress {
    final total = sizeBytes;
    if (total == null || total == 0) return 0;
    return (sentBytes / total).clamp(0, 1);
  }

  bool get isActive => switch (phase) {
    UploadPhase.choosing ||
    UploadPhase.importingUrl ||
    UploadPhase.uploading ||
    UploadPhase.completing ||
    UploadPhase.cancelling => true,
    _ => false,
  };

  UploadState copyWith({
    UploadPhase? phase,
    String? projectId,
    String? sourceUrl,
    String? filename,
    int? sizeBytes,
    int? sentBytes,
    UploadSession? session,
    QueuedJob? job,
    String? error,
    bool clearError = false,
  }) {
    return UploadState(
      phase: phase ?? this.phase,
      projectId: projectId ?? this.projectId,
      sourceUrl: sourceUrl ?? this.sourceUrl,
      filename: filename ?? this.filename,
      sizeBytes: sizeBytes ?? this.sizeBytes,
      sentBytes: sentBytes ?? this.sentBytes,
      session: session ?? this.session,
      job: job ?? this.job,
      error: clearError ? null : error ?? this.error,
    );
  }
}
