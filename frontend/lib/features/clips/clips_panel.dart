import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../import/import_models.dart';
import '../import/sources_controller.dart';
import 'clip_preview.dart';
import 'clip_render_controls.dart';
import 'clip_models.dart';
import 'clips_controller.dart';

class SuggestedClipsPanel extends ConsumerStatefulWidget {
  const SuggestedClipsPanel({required this.projectId, super.key});

  final String projectId;

  @override
  ConsumerState<SuggestedClipsPanel> createState() =>
      _SuggestedClipsPanelState();
}

class _SuggestedClipsPanelState extends ConsumerState<SuggestedClipsPanel> {
  @override
  void initState() {
    super.initState();
    Future.microtask(
      () => ref
          .read(clipsControllerProvider.notifier)
          .watchProject(widget.projectId),
    );
  }

  @override
  void didUpdateWidget(SuggestedClipsPanel oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.projectId == widget.projectId) return;
    Future.microtask(
      () => ref
          .read(clipsControllerProvider.notifier)
          .watchProject(widget.projectId),
    );
  }

  @override
  Widget build(BuildContext context) {
    ref.listen(sourcesControllerProvider, (previous, next) {
      if (previous?.projectId == widget.projectId &&
          previous!.hasPending &&
          !next.hasPending) {
        ref.read(clipsControllerProvider.notifier).refresh();
      }
    });
    final current = ref.watch(clipsControllerProvider);
    final state = current.projectId == widget.projectId
        ? current
        : ClipsState(projectId: widget.projectId, isLoading: true);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                const Icon(Icons.auto_awesome_outlined),
                const SizedBox(width: 12),
                Expanded(
                  child: Text(
                    'Suggested moments',
                    style: Theme.of(context).textTheme.titleLarge,
                  ),
                ),
                IconButton(
                  onPressed: state.isLoading
                      ? null
                      : () => ref
                            .read(clipsControllerProvider.notifier)
                            .refresh(),
                  icon: const Icon(Icons.refresh),
                  tooltip: 'Refresh suggestions',
                ),
              ],
            ),
            const SizedBox(height: 8),
            Text(
              'Preview the suggestions, select only the ones you want, then render them as 9:16 videos.',
              style: Theme.of(context).textTheme.bodyMedium,
            ),
            if (state.error != null) ...[
              const SizedBox(height: 16),
              Text(
                state.error!,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ],
            if (state.actionError != null) ...[
              const SizedBox(height: 12),
              Text(
                state.actionError!,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ],
            const SizedBox(height: 20),
            if (state.isLoading && !state.hasLoaded)
              const Center(child: CircularProgressIndicator())
            else if (state.clips.isEmpty)
              const Text(
                'Suggestions will appear here after subtitles and analysis finish.',
                textAlign: TextAlign.center,
              )
            else ...[
              ClipRenderToolbar(state: state),
              const SizedBox(height: 16),
              for (var index = 0; index < state.clips.length; index++) ...[
                _SuggestedClipCard(
                  index: index + 1,
                  clip: state.clips[index],
                  previewSelected: state.previewClipId == state.clips[index].id,
                  previewUrl: state.previewUrl,
                  renderedPreview: state.previewIsRendered,
                  transcript: state.transcript,
                  previewLoading: state.previewLoading,
                  transcriptLoading: state.transcriptLoading,
                  previewError: state.previewError,
                  transcriptError: state.transcriptError,
                  selected: state.selectedClipIds.contains(
                    state.clips[index].id,
                  ),
                  selectionLocked: state.renderLoading,
                  downloading: state.downloadingClipId == state.clips[index].id,
                  onSelection: () => ref
                      .read(clipsControllerProvider.notifier)
                      .toggleSelection(state.clips[index]),
                  onPreview: () => ref
                      .read(clipsControllerProvider.notifier)
                      .openPreview(state.clips[index]),
                  onDownload: () => ref
                      .read(clipsControllerProvider.notifier)
                      .download(state.clips[index]),
                ),
                if (index != state.clips.length - 1) const SizedBox(height: 12),
              ],
            ],
          ],
        ),
      ),
    );
  }
}

class _SuggestedClipCard extends StatelessWidget {
  const _SuggestedClipCard({
    required this.index,
    required this.clip,
    required this.previewSelected,
    required this.previewLoading,
    required this.renderedPreview,
    required this.transcriptLoading,
    required this.selected,
    required this.selectionLocked,
    required this.downloading,
    required this.onSelection,
    required this.onPreview,
    required this.onDownload,
    this.previewUrl,
    this.transcript,
    this.previewError,
    this.transcriptError,
  });

  final int index;
  final SuggestedClip clip;
  final bool previewSelected;
  final String? previewUrl;
  final TranscriptDocument? transcript;
  final bool previewLoading;
  final bool renderedPreview;
  final bool transcriptLoading;
  final bool selected;
  final bool selectionLocked;
  final bool downloading;
  final String? previewError;
  final String? transcriptError;
  final VoidCallback onSelection;
  final VoidCallback onPreview;
  final VoidCallback onDownload;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        border: Border.all(color: Theme.of(context).colorScheme.outlineVariant),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Tooltip(
                message: clip.canRender
                    ? clip.isRendered
                          ? 'Select to remake with the full-width 16:9 Shorts layout'
                          : 'Select this suggestion for rendering'
                    : 'This suggestion cannot be selected right now',
                child: Checkbox(
                  value: selected,
                  onChanged: clip.canRender && !selectionLocked
                      ? (_) => onSelection()
                      : null,
                ),
              ),
              const SizedBox(width: 4),
              CircleAvatar(radius: 16, child: Text('$index')),
              const SizedBox(width: 12),
              Expanded(
                child: Text(
                  clip.title,
                  style: Theme.of(context).textTheme.titleMedium,
                ),
              ),
              const SizedBox(width: 8),
              ClipStatusBadge(status: clip.status),
            ],
          ),
          if (clip.hook?.isNotEmpty == true) ...[
            const SizedBox(height: 12),
            Text(
              '“${clip.hook}”',
              style: Theme.of(context).textTheme.bodyLarge,
            ),
          ],
          if (clip.reason?.isNotEmpty == true) ...[
            const SizedBox(height: 8),
            Text(clip.reason!),
          ],
          const SizedBox(height: 10),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              Chip(
                avatar: const Icon(Icons.schedule, size: 16),
                label: Text(
                  '${_timestamp(clip.startMs)}–${_timestamp(clip.endMs)} '
                  '(${_duration(clip.durationMs)})',
                ),
              ),
              Chip(label: Text('${(clip.score * 100).round()}% match')),
              if (clip.contextRisk != null)
                Chip(label: Text('Context risk: ${clip.contextRisk}')),
            ],
          ),
          const SizedBox(height: 8),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              FilledButton.tonalIcon(
                onPressed: previewLoading && !previewSelected
                    ? null
                    : onPreview,
                icon: Icon(previewSelected ? Icons.close : Icons.play_circle),
                label: Text(previewSelected ? 'Close preview' : 'Preview clip'),
              ),
              if (clip.isRendered)
                FilledButton.icon(
                  onPressed: downloading ? null : onDownload,
                  icon: downloading
                      ? const SizedBox.square(
                          dimension: 16,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : const Icon(Icons.download),
                  label: const Text('Download MP4'),
                ),
            ],
          ),
          if (previewSelected) ...[
            const SizedBox(height: 12),
            const Divider(),
            const SizedBox(height: 8),
            Text(
              renderedPreview
                  ? 'Rendered 9:16 preview'
                  : 'Source segment preview',
              style: Theme.of(context).textTheme.labelLarge,
            ),
            const SizedBox(height: 10),
            if (previewLoading)
              const SizedBox(
                height: 180,
                child: Center(child: CircularProgressIndicator()),
              )
            else if (previewError != null)
              Text(
                previewError!,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              )
            else if (previewUrl != null)
              ClipPreview(
                key: ValueKey(clip.id),
                url: previewUrl!,
                startMs: renderedPreview ? 0 : clip.startMs,
                endMs: renderedPreview ? clip.durationMs : clip.endMs,
                transcript: renderedPreview ? null : transcript,
                transcriptLoading: renderedPreview ? false : transcriptLoading,
                transcriptError: renderedPreview ? null : transcriptError,
              ),
          ],
        ],
      ),
    );
  }

  String _timestamp(int milliseconds) {
    final totalSeconds = milliseconds ~/ 1000;
    final minutes = totalSeconds ~/ 60;
    final seconds = totalSeconds % 60;
    return '$minutes:${seconds.toString().padLeft(2, '0')}';
  }

  String _duration(int milliseconds) {
    final seconds = (milliseconds / 1000).round();
    return '${seconds}s';
  }
}
