import 'dart:async';

import 'package:flutter/material.dart';
import 'package:video_player/video_player.dart';

import '../import/import_models.dart';

class ClipPreview extends StatefulWidget {
  const ClipPreview({
    required this.url,
    required this.startMs,
    required this.endMs,
    this.transcript,
    this.transcriptLoading = false,
    this.transcriptError,
    super.key,
  });

  final String url;
  final int startMs;
  final int endMs;
  final TranscriptDocument? transcript;
  final bool transcriptLoading;
  final String? transcriptError;

  @override
  State<ClipPreview> createState() => _ClipPreviewState();
}

class _ClipPreviewState extends State<ClipPreview> {
  late VideoPlayerController _controller;
  late Future<void> _ready;
  int _positionMs = 0;
  int _activeCueIndex = -1;
  bool _wasPlaying = false;
  bool _resetting = false;

  @override
  void initState() {
    super.initState();
    _open();
  }

  @override
  void didUpdateWidget(ClipPreview oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.url != widget.url ||
        oldWidget.startMs != widget.startMs ||
        oldWidget.endMs != widget.endMs) {
      _controller.removeListener(_onVideoChanged);
      _controller.dispose();
      _open();
      return;
    }
    if (oldWidget.transcript != widget.transcript) {
      _activeCueIndex = _cueAt(_positionMs);
    }
  }

  void _open() {
    _positionMs = widget.startMs;
    _activeCueIndex = -1;
    _wasPlaying = false;
    _resetting = false;
    _controller = VideoPlayerController.networkUrl(Uri.parse(widget.url));
    _controller.addListener(_onVideoChanged);
    _ready = _controller.initialize().then((_) async {
      await _controller.setLooping(false);
      await _controller.seekTo(Duration(milliseconds: _segmentStart));
      _positionMs = _segmentStart;
      _activeCueIndex = _cueAt(_positionMs);
      if (mounted) setState(() {});
    });
  }

  int get _segmentStart {
    final duration = _controller.value.duration.inMilliseconds;
    return widget.startMs.clamp(0, duration).toInt();
  }

  int get _segmentEnd {
    final duration = _controller.value.duration.inMilliseconds;
    final requestedEnd = widget.endMs.clamp(0, duration).toInt();
    return requestedEnd > _segmentStart ? requestedEnd : duration;
  }

  void _onVideoChanged() {
    if (!mounted || !_controller.value.isInitialized) return;
    final positionMs = _controller.value.position.inMilliseconds;
    if (_controller.value.isPlaying &&
        positionMs >= _segmentEnd &&
        !_resetting) {
      unawaited(_finishSegment());
      return;
    }
    final nextCue = _cueAt(positionMs);
    final nextPlaying = _controller.value.isPlaying;
    if ((positionMs - _positionMs).abs() < 200 &&
        nextCue == _activeCueIndex &&
        nextPlaying == _wasPlaying) {
      return;
    }
    setState(() {
      _positionMs = positionMs.clamp(_segmentStart, _segmentEnd).toInt();
      _activeCueIndex = nextCue;
      _wasPlaying = nextPlaying;
    });
  }

  Future<void> _finishSegment() async {
    _resetting = true;
    await _controller.pause();
    await _controller.seekTo(Duration(milliseconds: _segmentStart));
    if (mounted) {
      setState(() {
        _positionMs = _segmentStart;
        _activeCueIndex = _cueAt(_segmentStart);
        _wasPlaying = false;
      });
    }
    _resetting = false;
  }

  int _cueAt(int positionMs) {
    final cues = widget.transcript?.cues ?? const <TranscriptCue>[];
    var low = 0;
    var high = cues.length - 1;
    while (low <= high) {
      final middle = (low + high) ~/ 2;
      final cue = cues[middle];
      if (positionMs < cue.startMs) {
        high = middle - 1;
      } else if (positionMs >= cue.endMs) {
        low = middle + 1;
      } else {
        return middle;
      }
    }
    return -1;
  }

  Future<void> _togglePlayback() async {
    if (_controller.value.isPlaying) {
      await _controller.pause();
      return;
    }
    if (_controller.value.position.inMilliseconds < _segmentStart ||
        _controller.value.position.inMilliseconds >= _segmentEnd) {
      await _controller.seekTo(Duration(milliseconds: _segmentStart));
    }
    await _controller.play();
  }

  Future<void> _seek(double milliseconds) async {
    await _controller.seekTo(Duration(milliseconds: milliseconds.round()));
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
          return const Padding(
            padding: EdgeInsets.symmetric(vertical: 16),
            child: Text('The suggested segment could not be played.'),
          );
        }
        if (snapshot.connectionState != ConnectionState.done) {
          return const SizedBox(
            height: 240,
            child: Center(child: CircularProgressIndicator()),
          );
        }
        final cues = widget.transcript?.cues ?? const <TranscriptCue>[];
        final activeCue = _activeCueIndex >= 0 ? cues[_activeCueIndex] : null;
        final position = _positionMs
            .clamp(_segmentStart, _segmentEnd)
            .toDouble();
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Center(
              child: ConstrainedBox(
                constraints: const BoxConstraints(maxHeight: 420),
                child: AspectRatio(
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
                            child: _ClipSubtitle(text: activeCue.text),
                          ),
                        Center(
                          child: AnimatedOpacity(
                            opacity: _controller.value.isPlaying ? 0 : 1,
                            duration: const Duration(milliseconds: 150),
                            child: IconButton.filled(
                              onPressed: _togglePlayback,
                              iconSize: 34,
                              icon: const Icon(Icons.play_arrow),
                              tooltip: 'Play suggested moment',
                            ),
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
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
                  tooltip: _controller.value.isPlaying ? 'Pause' : 'Play',
                ),
                Expanded(
                  child: Slider(
                    value: position,
                    min: _segmentStart.toDouble(),
                    max: _segmentEnd.toDouble(),
                    onChanged: _seek,
                  ),
                ),
                Text(
                  '${_duration(_positionMs - _segmentStart)} / '
                  '${_duration(_segmentEnd - _segmentStart)}',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ],
            ),
            if (widget.transcriptLoading)
              const Row(
                children: [
                  SizedBox.square(
                    dimension: 14,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  ),
                  SizedBox(width: 8),
                  Text('Loading subtitles…'),
                ],
              )
            else if (widget.transcriptError != null)
              Text(
                widget.transcriptError!,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
          ],
        );
      },
    );
  }
}

class _ClipSubtitle extends StatelessWidget {
  const _ClipSubtitle({required this.text});

  final String text;

  @override
  Widget build(BuildContext context) {
    return Container(
      constraints: const BoxConstraints(maxWidth: 680),
      margin: const EdgeInsets.symmetric(horizontal: 16),
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
      decoration: BoxDecoration(
        color: Colors.black.withValues(alpha: 0.8),
        borderRadius: BorderRadius.circular(9),
      ),
      child: Text(
        text,
        maxLines: 3,
        overflow: TextOverflow.ellipsis,
        textAlign: TextAlign.center,
        style: const TextStyle(
          color: Colors.white,
          fontSize: 17,
          height: 1.25,
          fontWeight: FontWeight.w600,
          shadows: [Shadow(blurRadius: 3, color: Colors.black)],
        ),
      ),
    );
  }
}

String _duration(int milliseconds) {
  final totalSeconds = (milliseconds < 0 ? 0 : milliseconds) ~/ 1000;
  final minutes = totalSeconds ~/ 60;
  final seconds = totalSeconds % 60;
  return '$minutes:${seconds.toString().padLeft(2, '0')}';
}
