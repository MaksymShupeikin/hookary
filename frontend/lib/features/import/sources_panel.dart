import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:intl/intl.dart';

import 'import_models.dart';
import 'source_preview.dart';
import 'sources_controller.dart';

class ProjectSourcesPanel extends ConsumerStatefulWidget {
  const ProjectSourcesPanel({required this.projectId, super.key});

  final String projectId;

  @override
  ConsumerState<ProjectSourcesPanel> createState() =>
      _ProjectSourcesPanelState();
}

class _ProjectSourcesPanelState extends ConsumerState<ProjectSourcesPanel> {
  @override
  void initState() {
    super.initState();
    Future.microtask(
      () => ref
          .read(sourcesControllerProvider.notifier)
          .watchProject(widget.projectId),
    );
  }

  @override
  void didUpdateWidget(ProjectSourcesPanel oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.projectId == widget.projectId) return;
    Future.microtask(
      () => ref
          .read(sourcesControllerProvider.notifier)
          .watchProject(widget.projectId),
    );
  }

  @override
  Widget build(BuildContext context) {
    final current = ref.watch(sourcesControllerProvider);
    final state = current.projectId == widget.projectId
        ? current
        : SourcesState(projectId: widget.projectId, isLoading: true);

    return Card(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                const Icon(Icons.video_library_outlined),
                const SizedBox(width: 12),
                Expanded(
                  child: Text(
                    'Project sources',
                    style: Theme.of(context).textTheme.titleLarge,
                  ),
                ),
                IconButton(
                  onPressed: state.isLoading
                      ? null
                      : () => ref
                            .read(sourcesControllerProvider.notifier)
                            .refresh(),
                  icon: const Icon(Icons.refresh),
                  tooltip: 'Refresh sources',
                ),
              ],
            ),
            const SizedBox(height: 8),
            Text(
              'Every video already added to this project stays here.',
              style: Theme.of(context).textTheme.bodyMedium,
            ),
            if (state.error != null) ...[
              const SizedBox(height: 16),
              Text(
                state.error!,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ],
            const SizedBox(height: 20),
            if (state.isLoading && !state.hasLoaded)
              const Center(child: CircularProgressIndicator())
            else if (state.assets.isEmpty)
              const _EmptySources()
            else
              for (var index = 0; index < state.assets.length; index++) ...[
                _SourceCard(asset: state.assets[index], state: state),
                if (index != state.assets.length - 1)
                  const SizedBox(height: 12),
              ],
          ],
        ),
      ),
    );
  }
}

class _EmptySources extends StatelessWidget {
  const _EmptySources();

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(20),
      decoration: BoxDecoration(
        color: Theme.of(context).colorScheme.surfaceContainerLow,
        borderRadius: BorderRadius.circular(12),
      ),
      child: const Text(
        'No source videos yet. Add a YouTube link or upload a file below.',
        textAlign: TextAlign.center,
      ),
    );
  }
}

class _SourceCard extends ConsumerWidget {
  const _SourceCard({required this.asset, required this.state});

  final SourceAsset asset;
  final SourcesState state;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final selected = state.selectedAssetId == asset.id;
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        border: Border.all(color: Theme.of(context).colorScheme.outlineVariant),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Icon(
                asset.kind == SourceKind.publicUrl
                    ? Icons.smart_display_outlined
                    : Icons.video_file_outlined,
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      asset.title,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                    const SizedBox(height: 4),
                    Text(
                      _details(asset),
                      style: Theme.of(context).textTheme.bodySmall,
                    ),
                  ],
                ),
              ),
              const SizedBox(width: 12),
              _StatusBadge(status: asset.status),
            ],
          ),
          const SizedBox(height: 12),
          Row(
            children: [
              Expanded(child: Text(_availability(asset))),
              if (asset.status == SourceStatus.ready &&
                  !asset.transcriptReady &&
                  asset.transcriptStatus == TranscriptStatus.failed)
                TextButton.icon(
                  onPressed: () => ref
                      .read(sourcesControllerProvider.notifier)
                      .startAnalysis(asset),
                  icon: const Icon(Icons.refresh),
                  label: const Text('Prepare subtitles'),
                ),
              if (selected)
                TextButton.icon(
                  onPressed: () => ref
                      .read(sourcesControllerProvider.notifier)
                      .closePreview(),
                  icon: const Icon(Icons.close),
                  label: const Text('Close'),
                )
              else
                FilledButton.tonalIcon(
                  onPressed: asset.previewReady && !state.previewLoading
                      ? () => ref
                            .read(sourcesControllerProvider.notifier)
                            .openPreview(asset)
                      : null,
                  icon: const Icon(Icons.play_arrow),
                  label: const Text('Preview'),
                ),
            ],
          ),
          if (selected) ...[
            const SizedBox(height: 12),
            if (state.previewLoading)
              const AspectRatio(
                aspectRatio: 16 / 9,
                child: Center(child: CircularProgressIndicator()),
              )
            else if (state.previewError != null)
              Text(
                state.previewError!,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              )
            else if (state.previewUrl != null)
              SourcePreview(
                key: ValueKey(asset.id),
                url: state.previewUrl!,
                transcript: state.transcript,
                transcriptStatus: asset.transcriptStatus,
                transcriptLoading: state.transcriptLoading,
                transcriptError: state.transcriptError,
              ),
          ],
        ],
      ),
    );
  }

  String _details(SourceAsset asset) {
    final details = <String>[
      asset.kind == SourceKind.publicUrl ? 'YouTube' : 'Local upload',
    ];
    if (asset.durationMs != null) details.add(_duration(asset.durationMs!));
    if (asset.sizeBytes != null) details.add(_bytes(asset.sizeBytes!));
    if (asset.width != null && asset.height != null) {
      details.add('${asset.width}×${asset.height}');
    }
    details.add(DateFormat('MMM d, HH:mm').format(asset.createdAt.toLocal()));
    return details.join(' · ');
  }

  String _availability(SourceAsset asset) => switch (asset.status) {
    SourceStatus.pending when asset.previewReady =>
      'Uploaded. Processing continues in the background.',
    SourceStatus.pending => 'Downloading and preparing the source video…',
    SourceStatus.ready
        when asset.transcriptReady &&
            (asset.analysisStatus == TranscriptStatus.pending ||
                asset.analysisStatus == TranscriptStatus.queued ||
                asset.analysisStatus == TranscriptStatus.running) =>
      'Video and subtitles are ready. Finding the strongest moments…',
    SourceStatus.ready
        when asset.transcriptReady &&
            asset.analysisStatus == TranscriptStatus.failed =>
      'Video and subtitles are ready, but moment selection failed.',
    SourceStatus.ready when asset.transcriptReady =>
      'Video, subtitles, and suggested moments are ready.',
    SourceStatus.ready when asset.transcriptStatus == TranscriptStatus.failed =>
      'Video is ready, but subtitle preparation failed.',
    SourceStatus.ready =>
      'Video is ready. Subtitles and interesting moments are being prepared…',
    SourceStatus.failed => 'This source could not be prepared.',
  };

  String _duration(int milliseconds) {
    final totalSeconds = milliseconds ~/ 1000;
    final minutes = totalSeconds ~/ 60;
    final seconds = totalSeconds % 60;
    return '$minutes:${seconds.toString().padLeft(2, '0')}';
  }

  String _bytes(int bytes) {
    const units = ['B', 'KiB', 'MiB', 'GiB'];
    var value = bytes.toDouble();
    var unit = 0;
    while (value >= 1024 && unit < units.length - 1) {
      value /= 1024;
      unit++;
    }
    return '${value.toStringAsFixed(unit == 0 ? 0 : 1)} ${units[unit]}';
  }
}

class _StatusBadge extends StatelessWidget {
  const _StatusBadge({required this.status});

  final SourceStatus status;

  @override
  Widget build(BuildContext context) {
    final (label, color) = switch (status) {
      SourceStatus.pending => ('Processing', Colors.orange),
      SourceStatus.ready => ('Ready', Colors.green),
      SourceStatus.failed => ('Failed', Theme.of(context).colorScheme.error),
    };
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 5),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(label, style: TextStyle(color: color)),
    );
  }
}
