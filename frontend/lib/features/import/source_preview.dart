import 'package:flutter/material.dart';
import 'package:video_player/video_player.dart';

import 'import_models.dart';

class SourcePreview extends StatefulWidget {
  const SourcePreview({
    required this.url,
    required this.transcriptStatus,
    this.transcript,
    this.transcriptLoading = false,
    this.transcriptError,
    super.key,
  });

  final String url;
  final TranscriptDocument? transcript;
  final TranscriptStatus transcriptStatus;
  final bool transcriptLoading;
  final String? transcriptError;

  @override
  State<SourcePreview> createState() => _SourcePreviewState();
}

class _SourcePreviewState extends State<SourcePreview> {
  late VideoPlayerController _controller;
  late Future<void> _ready;
  int _activeCueIndex = -1;
  int _positionSecond = 0;
  bool _wasPlaying = false;

  @override
  void initState() {
    super.initState();
    _open();
  }

  @override
  void didUpdateWidget(SourcePreview oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.url != widget.url) {
      _controller.removeListener(_onVideoChanged);
      _controller.dispose();
      _open();
      return;
    }
    if (oldWidget.transcript != widget.transcript) {
      _activeCueIndex = _cueAt(_controller.value.position.inMilliseconds);
    }
  }

  void _open() {
    _activeCueIndex = -1;
    _positionSecond = 0;
    _wasPlaying = false;
    _controller = VideoPlayerController.networkUrl(Uri.parse(widget.url));
    _controller.addListener(_onVideoChanged);
    _ready = _controller.initialize().then((_) {
      _controller.setLooping(true);
      if (mounted) setState(() {});
    });
  }

  void _onVideoChanged() {
    if (!mounted || !_controller.value.isInitialized) return;
    final position = _controller.value.position;
    final nextCue = _cueAt(position.inMilliseconds);
    final nextSecond = position.inSeconds;
    final nextPlaying = _controller.value.isPlaying;
    if (nextCue == _activeCueIndex &&
        nextSecond == _positionSecond &&
        nextPlaying == _wasPlaying) {
      return;
    }
    setState(() {
      _activeCueIndex = nextCue;
      _positionSecond = nextSecond;
      _wasPlaying = nextPlaying;
    });
  }

  int _cueAt(int positionMS) {
    final cues = widget.transcript?.cues ?? const <TranscriptCue>[];
    if (cues.isEmpty) return -1;
    var low = 0;
    var high = cues.length - 1;
    while (low <= high) {
      final middle = (low + high) ~/ 2;
      final cue = cues[middle];
      if (positionMS < cue.startMs) {
        high = middle - 1;
      } else if (positionMS >= cue.endMs) {
        low = middle + 1;
      } else {
        return middle;
      }
    }
    return -1;
  }

  Future<void> _seekTo(TranscriptCue cue) async {
    await _controller.seekTo(Duration(milliseconds: cue.startMs));
    await _controller.play();
  }

  @override
  void dispose() {
    _controller.removeListener(_onVideoChanged);
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return FutureBuilder<void>(
      future: _ready,
      builder: (context, snapshot) {
        if (snapshot.hasError) {
          return const Text('The preview URL could not be played.');
        }
        if (snapshot.connectionState != ConnectionState.done) {
          return const AspectRatio(
            aspectRatio: 16 / 9,
            child: Center(child: CircularProgressIndicator()),
          );
        }
        final cues = widget.transcript?.cues ?? const <TranscriptCue>[];
        final activeCue = _activeCueIndex >= 0 ? cues[_activeCueIndex] : null;
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            AspectRatio(
              aspectRatio: _controller.value.aspectRatio,
              child: ClipRRect(
                borderRadius: BorderRadius.circular(12),
                child: Stack(
                  fit: StackFit.expand,
                  children: [
                    VideoPlayer(_controller),
                    if (activeCue != null)
                      Align(
                        alignment: const Alignment(0, 0.82),
                        child: _SubtitleOverlay(text: activeCue.text),
                      ),
                    Center(
                      child: AnimatedOpacity(
                        opacity: _controller.value.isPlaying ? 0 : 1,
                        duration: const Duration(milliseconds: 150),
                        child: IconButton.filled(
                          onPressed: _togglePlayback,
                          iconSize: 36,
                          icon: const Icon(Icons.play_arrow),
                          tooltip: 'Play preview',
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            ),
            VideoProgressIndicator(
              _controller,
              allowScrubbing: true,
              padding: const EdgeInsets.only(top: 10),
            ),
            Row(
              children: [
                IconButton(
                  onPressed: _togglePlayback,
                  icon: Icon(
                    _controller.value.isPlaying
                        ? Icons.pause
                        : Icons.play_arrow,
                  ),
                  tooltip: _controller.value.isPlaying
                      ? 'Pause preview'
                      : 'Play preview',
                ),
                Text(
                  '${_timestamp(_positionSecond * 1000)} / '
                  '${_timestamp(_controller.value.duration.inMilliseconds)}',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ],
            ),
            _TranscriptReader(
              key: ValueKey(
                widget.transcript?.source ?? widget.transcriptStatus,
              ),
              transcript: widget.transcript,
              status: widget.transcriptStatus,
              isLoading: widget.transcriptLoading,
              error: widget.transcriptError,
              activeCueIndex: _activeCueIndex,
              onCueSelected: _seekTo,
            ),
          ],
        );
      },
    );
  }

  void _togglePlayback() {
    _controller.value.isPlaying ? _controller.pause() : _controller.play();
  }
}

class _SubtitleOverlay extends StatelessWidget {
  const _SubtitleOverlay({required this.text});

  final String text;

  @override
  Widget build(BuildContext context) {
    return Container(
      constraints: const BoxConstraints(maxWidth: 720),
      margin: const EdgeInsets.symmetric(horizontal: 20),
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
      decoration: BoxDecoration(
        color: Colors.black.withValues(alpha: 0.78),
        borderRadius: BorderRadius.circular(10),
      ),
      child: Text(
        text,
        maxLines: 3,
        overflow: TextOverflow.ellipsis,
        textAlign: TextAlign.center,
        style: const TextStyle(
          color: Colors.white,
          fontSize: 18,
          height: 1.25,
          fontWeight: FontWeight.w600,
          shadows: [Shadow(blurRadius: 3, color: Colors.black)],
        ),
      ),
    );
  }
}

class _TranscriptReader extends StatelessWidget {
  const _TranscriptReader({
    required this.transcript,
    required this.status,
    required this.isLoading,
    required this.error,
    required this.activeCueIndex,
    required this.onCueSelected,
    super.key,
  });

  final TranscriptDocument? transcript;
  final TranscriptStatus status;
  final bool isLoading;
  final String? error;
  final int activeCueIndex;
  final ValueChanged<TranscriptCue> onCueSelected;

  @override
  Widget build(BuildContext context) {
    final document = transcript;
    return ExpansionTile(
      tilePadding: EdgeInsets.zero,
      initiallyExpanded: document != null,
      leading: const Icon(Icons.subtitles_outlined),
      title: const Text('Readable transcript'),
      subtitle: Text(_subtitle(document)),
      children: [
        if (isLoading)
          const Padding(
            padding: EdgeInsets.all(20),
            child: Center(child: CircularProgressIndicator()),
          )
        else if (error != null)
          Padding(
            padding: const EdgeInsets.only(bottom: 16),
            child: Text(
              error!,
              style: TextStyle(color: Theme.of(context).colorScheme.error),
            ),
          )
        else if (document == null)
          Padding(
            padding: const EdgeInsets.only(bottom: 16),
            child: Text(_emptyMessage()),
          )
        else
          ConstrainedBox(
            constraints: const BoxConstraints(maxHeight: 320),
            child: ListView.builder(
              shrinkWrap: true,
              itemCount: document.cues.length,
              itemBuilder: (context, index) {
                final cue = document.cues[index];
                final active = index == activeCueIndex;
                return Material(
                  color: active
                      ? Theme.of(
                          context,
                        ).colorScheme.primaryContainer.withValues(alpha: 0.6)
                      : Colors.transparent,
                  borderRadius: BorderRadius.circular(8),
                  child: ListTile(
                    dense: true,
                    selected: active,
                    leading: Text(
                      _timestamp(cue.startMs),
                      style: Theme.of(context).textTheme.labelMedium,
                    ),
                    title: Text(cue.text),
                    onTap: () => onCueSelected(cue),
                  ),
                );
              },
            ),
          ),
      ],
    );
  }

  String _subtitle(TranscriptDocument? document) {
    if (document != null) {
      final language = document.language.isEmpty
          ? ''
          : ' · ${document.language}';
      return '${document.sourceLabel}$language · tap a line to seek';
    }
    return switch (status) {
      TranscriptStatus.pending ||
      TranscriptStatus.queued ||
      TranscriptStatus.running => 'Subtitles are being prepared',
      TranscriptStatus.failed => 'Subtitle preparation failed',
      TranscriptStatus.succeeded => 'Subtitles are ready',
    };
  }

  String _emptyMessage() => switch (status) {
    TranscriptStatus.pending ||
    TranscriptStatus.queued ||
    TranscriptStatus.running =>
      'The video is ready to watch. Timed subtitles will appear here automatically.',
    TranscriptStatus.failed =>
      'Hookary could not create subtitles for this source. Check the processing job for details.',
    TranscriptStatus.succeeded =>
      'Reopen the preview to load the completed transcript.',
  };
}

String _timestamp(int milliseconds) {
  final totalSeconds = milliseconds ~/ 1000;
  final hours = totalSeconds ~/ 3600;
  final minutes = (totalSeconds % 3600) ~/ 60;
  final seconds = totalSeconds % 60;
  final minuteText = hours > 0
      ? minutes.toString().padLeft(2, '0')
      : minutes.toString();
  final prefix = hours > 0 ? '$hours:' : '';
  return '$prefix$minuteText:${seconds.toString().padLeft(2, '0')}';
}
