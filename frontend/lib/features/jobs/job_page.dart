import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/router/app_router.dart';
import 'job_models.dart';
import 'jobs_controller.dart';

class JobPage extends ConsumerStatefulWidget {
  const JobPage({required this.projectId, super.key});

  final String projectId;

  @override
  ConsumerState<JobPage> createState() => _JobPageState();
}

class _JobPageState extends ConsumerState<JobPage> {
  @override
  void initState() {
    super.initState();
    Future.microtask(
      () => ref
          .read(jobsControllerProvider.notifier)
          .watchProject(widget.projectId),
    );
  }

  @override
  Widget build(BuildContext context) {
    final state = ref.watch(jobsControllerProvider);
    final visible = state.projectId == widget.projectId
        ? state
        : const JobsState(isLoading: true);

    return Scaffold(
      appBar: AppBar(
        leading: IconButton(
          onPressed: () => context.go(Routes.project(widget.projectId)),
          icon: const Icon(Icons.arrow_back),
        ),
        title: const Text('Processing'),
        actions: [
          IconButton(
            onPressed: visible.isLoading
                ? null
                : () => ref.read(jobsControllerProvider.notifier).refresh(),
            icon: const Icon(Icons.refresh),
            tooltip: 'Refresh',
          ),
          const SizedBox(width: 8),
        ],
      ),
      body: ListView(
        padding: const EdgeInsets.all(24),
        children: [
          Center(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 860),
              child: _Pipeline(state: visible),
            ),
          ),
        ],
      ),
    );
  }
}

class _Pipeline extends ConsumerWidget {
  const _Pipeline({required this.state});

  final JobsState state;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final jobsByStage = <JobStage, Job>{};
    for (final job in state.jobs) {
      jobsByStage[job.stage] = job;
    }

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            Expanded(
              child: Text(
                'Video pipeline',
                style: Theme.of(context).textTheme.headlineSmall,
              ),
            ),
            _ConnectionBadge(isLive: state.isLive),
          ],
        ),
        const SizedBox(height: 8),
        Text(_summary(state), style: Theme.of(context).textTheme.bodyMedium),
        if (state.error != null) ...[
          const SizedBox(height: 16),
          Text(
            state.error!,
            style: TextStyle(color: Theme.of(context).colorScheme.error),
          ),
        ],
        const SizedBox(height: 24),
        if (state.isLoading && !state.hasLoaded)
          const Center(child: CircularProgressIndicator())
        else if (state.jobs.isEmpty)
          const _EmptyPipeline()
        else
          for (final stage in JobStage.values) ...[
            _StageCard(
              stage: stage,
              job: jobsByStage[stage],
              isRetrying: state.retryingJobId == jobsByStage[stage]?.id,
              onRetry: jobsByStage[stage]?.status == JobStatus.failed
                  ? () => ref
                        .read(jobsControllerProvider.notifier)
                        .retry(jobsByStage[stage]!.id)
                  : null,
            ),
            if (stage != JobStage.publish) const SizedBox(height: 12),
          ],
      ],
    );
  }

  String _summary(JobsState state) {
    if (state.jobs.any((job) => job.status == JobStatus.failed)) {
      return 'Processing stopped. Retry the failed stage when you are ready.';
    }
    if (state.jobs.any(
      (job) =>
          job.stage == JobStage.publish && job.status == JobStatus.succeeded,
    )) {
      return 'Source analysis completed. Read the subtitles and review the '
          'suggested moments on the project page.';
    }
    if (state.hasActiveJobs) {
      return 'Hookary is preparing the source, subtitles, and strongest '
          'short-form moments. Progress survives worker restarts.';
    }
    return 'Upload a source video to start processing.';
  }
}

class _ConnectionBadge extends StatelessWidget {
  const _ConnectionBadge({required this.isLive});

  final bool isLive;

  @override
  Widget build(BuildContext context) {
    final color = isLive ? Colors.green : Colors.orange;
    return Semantics(
      label: isLive ? 'Live updates connected' : 'Using polling updates',
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
        decoration: BoxDecoration(
          color: color.withValues(alpha: 0.12),
          borderRadius: BorderRadius.circular(99),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(isLive ? Icons.sensors : Icons.sync, size: 16, color: color),
            const SizedBox(width: 6),
            Text(isLive ? 'Live' : 'Polling'),
          ],
        ),
      ),
    );
  }
}

class _StageCard extends StatelessWidget {
  const _StageCard({
    required this.stage,
    required this.job,
    required this.isRetrying,
    required this.onRetry,
  });

  final JobStage stage;
  final Job? job;
  final bool isRetrying;
  final VoidCallback? onRetry;

  @override
  Widget build(BuildContext context) {
    final status = job?.status;
    final color = _color(status, Theme.of(context).colorScheme);
    return Card(
      key: ValueKey('stage-${stage.name}'),
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            CircleAvatar(
              backgroundColor: color.withValues(alpha: 0.12),
              foregroundColor: color,
              child: Icon(_icon(status)),
            ),
            const SizedBox(width: 16),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Row(
                    children: [
                      Expanded(
                        child: Text(
                          _title(stage),
                          style: Theme.of(context).textTheme.titleMedium,
                        ),
                      ),
                      Text(_status(status)),
                    ],
                  ),
                  const SizedBox(height: 4),
                  Text(_description(stage)),
                  if (status == JobStatus.running) ...[
                    const SizedBox(height: 12),
                    LinearProgressIndicator(value: job?.progress),
                  ],
                  if (job?.attempt != null && job!.attempt > 0) ...[
                    const SizedBox(height: 8),
                    Text(
                      'Attempt ${job!.attempt}',
                      style: Theme.of(context).textTheme.bodySmall,
                    ),
                  ],
                  if (job?.error != null) ...[
                    const SizedBox(height: 8),
                    Text(
                      job!.error!.message,
                      style: TextStyle(
                        color: Theme.of(context).colorScheme.error,
                      ),
                    ),
                  ],
                  if (onRetry != null) ...[
                    const SizedBox(height: 12),
                    Align(
                      alignment: Alignment.centerLeft,
                      child: FilledButton.tonalIcon(
                        onPressed: isRetrying ? null : onRetry,
                        icon: isRetrying
                            ? const SizedBox.square(
                                dimension: 16,
                                child: CircularProgressIndicator(
                                  strokeWidth: 2,
                                ),
                              )
                            : const Icon(Icons.refresh),
                        label: const Text('Retry stage'),
                      ),
                    ),
                  ],
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  String _title(JobStage value) => switch (value) {
    JobStage.ingest => 'Ingest',
    JobStage.analyze => 'Analyze',
    JobStage.render => 'Render',
    JobStage.publish => 'Publish',
  };

  String _description(JobStage value) => switch (value) {
    JobStage.ingest => 'Validate and normalize the source video.',
    JobStage.analyze => 'Transcribe and select the strongest moments.',
    JobStage.render => 'Compose vertical clips and subtitles.',
    JobStage.publish => 'Prepare the result for download and publishing.',
  };

  String _status(JobStatus? value) => switch (value) {
    null => 'Waiting',
    JobStatus.queued => 'Queued',
    JobStatus.running => 'Running',
    JobStatus.succeeded => 'Done',
    JobStatus.failed => 'Failed',
  };

  IconData _icon(JobStatus? value) => switch (value) {
    null => Icons.more_horiz,
    JobStatus.queued => Icons.schedule,
    JobStatus.running => Icons.autorenew,
    JobStatus.succeeded => Icons.check,
    JobStatus.failed => Icons.error_outline,
  };

  Color _color(JobStatus? value, ColorScheme colors) => switch (value) {
    null => colors.outline,
    JobStatus.queued => colors.secondary,
    JobStatus.running => colors.primary,
    JobStatus.succeeded => Colors.green,
    JobStatus.failed => colors.error,
  };
}

class _EmptyPipeline extends StatelessWidget {
  const _EmptyPipeline();

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          children: [
            Icon(
              Icons.video_file_outlined,
              size: 40,
              color: Theme.of(context).colorScheme.outline,
            ),
            const SizedBox(height: 12),
            const Text('No processing jobs yet.'),
          ],
        ),
      ),
    );
  }
}
