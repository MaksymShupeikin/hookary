import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'clip_models.dart';
import 'clips_controller.dart';

class ClipRenderToolbar extends ConsumerWidget {
  const ClipRenderToolbar({required this.state, super.key});

  final ClipsState state;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final renderable = state.clips.where((clip) => clip.canRender).toList();
    final allSelected =
        renderable.isNotEmpty &&
        renderable.every((clip) => state.selectedClipIds.contains(clip.id));
    final count = state.selectedClipIds.length;
    return DecoratedBox(
      decoration: BoxDecoration(
        color: Theme.of(context).colorScheme.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(12),
      ),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Wrap(
          spacing: 12,
          runSpacing: 10,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [
            TextButton.icon(
              onPressed: renderable.isEmpty || state.renderLoading
                  ? null
                  : () => ref
                        .read(clipsControllerProvider.notifier)
                        .selectAllRenderable(),
              icon: Icon(allSelected ? Icons.deselect : Icons.select_all),
              label: Text(allSelected ? 'Clear selection' : 'Select all'),
            ),
            FilledButton.icon(
              onPressed: count == 0 || state.renderLoading
                  ? null
                  : () => ref
                        .read(clipsControllerProvider.notifier)
                        .renderSelected(),
              icon: state.renderLoading
                  ? const SizedBox.square(
                      dimension: 16,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Icon(Icons.movie_creation_outlined),
              label: Text('Render selected ($count)'),
            ),
            Text(
              '9:16 Short · full-width 16:9 video · ambient top/bottom · subtitles',
              style: Theme.of(context).textTheme.bodySmall,
            ),
          ],
        ),
      ),
    );
  }
}

class ClipStatusBadge extends StatelessWidget {
  const ClipStatusBadge({required this.status, super.key});

  final String status;

  @override
  Widget build(BuildContext context) {
    final (label, icon) = switch (status) {
      'rendering' => ('Rendering', Icons.hourglass_top),
      'rendered' => ('Ready', Icons.check_circle_outline),
      'failed' => ('Failed', Icons.error_outline),
      _ => (status == 'candidate' ? 'Suggested' : status, Icons.auto_awesome),
    };
    return Chip(
      avatar: status == 'rendering'
          ? const SizedBox.square(
              dimension: 14,
              child: CircularProgressIndicator(strokeWidth: 2),
            )
          : Icon(icon, size: 16),
      label: Text(label),
      visualDensity: VisualDensity.compact,
    );
  }
}
