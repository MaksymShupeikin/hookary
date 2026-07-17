import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/router/app_router.dart';
import 'import_controller.dart';
import 'import_models.dart';

class UploadPanel extends ConsumerWidget {
  const UploadPanel({required this.projectId, super.key});

  final String projectId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final current = ref.watch(importControllerProvider);
    final state = current.projectId == null || current.projectId == projectId
        ? current
        : const UploadState();
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                const Icon(Icons.smart_display_outlined),
                const SizedBox(width: 12),
                Expanded(
                  child: Text(
                    'Add a source video',
                    style: Theme.of(context).textTheme.titleLarge,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 8),
            Text(
              'Paste a public YouTube link to download the video for '
              'processing. You can also upload your own file.',
              style: Theme.of(context).textTheme.bodyMedium,
            ),
            const SizedBox(height: 20),
            if (state.phase == UploadPhase.idle)
              _YouTubeSourceChooser(projectId: projectId)
            else ...[
              _UploadStatus(state: state),
              const SizedBox(height: 16),
              _UploadActions(state: state),
            ],
            if (state.error != null) ...[
              const SizedBox(height: 12),
              Text(
                state.error!,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _UploadStatus extends StatelessWidget {
  const _UploadStatus({required this.state});

  final UploadState state;

  @override
  Widget build(BuildContext context) {
    final showProgress =
        state.phase != UploadPhase.completed && state.sizeBytes != null;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text(
          state.filename ?? _sourceLabel(state.sourceUrl),
          style: Theme.of(context).textTheme.titleMedium,
        ),
        const SizedBox(height: 4),
        Text(_label(state)),
        if (showProgress) ...[
          const SizedBox(height: 12),
          LinearProgressIndicator(value: state.progress),
          const SizedBox(height: 6),
          Text(
            '${_bytes(state.sentBytes)} of ${_bytes(state.sizeBytes!)} '
            '(${(state.progress * 100).toStringAsFixed(0)}%)',
            textAlign: TextAlign.end,
            style: Theme.of(context).textTheme.bodySmall,
          ),
        ],
      ],
    );
  }

  String _label(UploadState state) => switch (state.phase) {
    UploadPhase.choosing => 'Choosing a file…',
    UploadPhase.importingUrl => 'Checking YouTube and queuing the download…',
    UploadPhase.uploading =>
      state.session?.mode == UploadMode.multipart
          ? 'Uploading in resumable parts…'
          : 'Uploading…',
    UploadPhase.paused => 'Paused',
    UploadPhase.completing => 'Verifying the object with storage…',
    UploadPhase.completed =>
      state.sourceUrl == null
          ? 'Upload verified · ingest job queued'
          : 'YouTube download queued',
    UploadPhase.cancelling => 'Cancelling…',
    UploadPhase.failed => 'Upload stopped',
    UploadPhase.idle => '',
  };

  String _sourceLabel(String? sourceUrl) {
    final host = sourceUrl == null ? null : Uri.tryParse(sourceUrl)?.host;
    return host == null || host.isEmpty ? 'Video' : host;
  }

  static String _bytes(int bytes) {
    const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
    var value = bytes.toDouble();
    var unit = 0;
    while (value >= 1024 && unit < units.length - 1) {
      value /= 1024;
      unit++;
    }
    return '${value.toStringAsFixed(unit == 0 ? 0 : 1)} ${units[unit]}';
  }
}

class _YouTubeSourceChooser extends ConsumerStatefulWidget {
  const _YouTubeSourceChooser({required this.projectId});

  final String projectId;

  @override
  ConsumerState<_YouTubeSourceChooser> createState() =>
      _YouTubeSourceChooserState();
}

class _YouTubeSourceChooserState extends ConsumerState<_YouTubeSourceChooser> {
  final _url = TextEditingController();

  @override
  void dispose() {
    _url.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final controller = ref.read(importControllerProvider.notifier);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text(
          'Import from YouTube',
          style: Theme.of(context).textTheme.titleMedium,
        ),
        const SizedBox(height: 8),
        TextField(
          controller: _url,
          keyboardType: TextInputType.url,
          autocorrect: false,
          decoration: const InputDecoration(
            labelText: 'YouTube link',
            hintText: 'https://www.youtube.com/watch?v=…',
            prefixIcon: Icon(Icons.smart_display_outlined),
          ),
          onSubmitted: (_) =>
              controller.importYouTube(widget.projectId, _url.text),
        ),
        const SizedBox(height: 12),
        FilledButton.icon(
          onPressed: () =>
              controller.importYouTube(widget.projectId, _url.text),
          icon: const Icon(Icons.download_outlined),
          label: const Text('Download from YouTube'),
        ),
        const Padding(
          padding: EdgeInsets.symmetric(vertical: 16),
          child: Row(
            children: [
              Expanded(child: Divider()),
              Padding(
                padding: EdgeInsets.symmetric(horizontal: 12),
                child: Text('OR UPLOAD YOUR OWN VIDEO'),
              ),
              Expanded(child: Divider()),
            ],
          ),
        ),
        OutlinedButton.icon(
          onPressed: () => controller.chooseAndUpload(widget.projectId),
          icon: const Icon(Icons.video_file_outlined),
          label: const Text('Choose a local video'),
        ),
      ],
    );
  }
}

class _UploadActions extends ConsumerWidget {
  const _UploadActions({required this.state});

  final UploadState state;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final controller = ref.read(importControllerProvider.notifier);
    return Wrap(
      spacing: 8,
      runSpacing: 8,
      children: [
        if (state.phase == UploadPhase.uploading)
          OutlinedButton.icon(
            onPressed: controller.pause,
            icon: const Icon(Icons.pause),
            label: const Text('Pause'),
          ),
        if (state.phase == UploadPhase.paused)
          FilledButton.icon(
            onPressed: controller.resume,
            icon: const Icon(Icons.play_arrow),
            label: const Text('Resume'),
          ),
        if (state.phase == UploadPhase.failed)
          FilledButton.icon(
            onPressed: controller.retry,
            icon: const Icon(Icons.refresh),
            label: const Text('Retry'),
          ),
        if (state.phase == UploadPhase.uploading ||
            state.phase == UploadPhase.paused ||
            state.phase == UploadPhase.failed)
          TextButton.icon(
            onPressed: controller.cancel,
            icon: const Icon(Icons.close),
            label: const Text('Cancel'),
          ),
        if (state.phase == UploadPhase.completed)
          FilledButton.tonalIcon(
            onPressed: state.projectId == null
                ? null
                : () => context.go(Routes.processing(state.projectId!)),
            icon: const Icon(Icons.account_tree_outlined),
            label: const Text('View processing'),
          ),
        if (state.phase == UploadPhase.completed)
          TextButton.icon(
            onPressed: controller.clearCompleted,
            icon: const Icon(Icons.add),
            label: const Text('Add another source'),
          ),
      ],
    );
  }
}
