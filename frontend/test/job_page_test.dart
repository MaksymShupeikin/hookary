import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hookary/features/jobs/job_models.dart';
import 'package:hookary/features/jobs/job_page.dart';
import 'package:hookary/features/jobs/jobs_controller.dart';

const projectId = '01JZ8WKQ4T6N9R2VBC3D5EFGHJ';

class _FixedJobsController extends JobsController {
  @override
  JobsState build() {
    final now = DateTime.utc(2026, 7, 16, 9, 15);
    return JobsState(
      projectId: projectId,
      hasLoaded: true,
      isLive: true,
      jobs: [
        Job(
          id: '01JZ8XKQ4T6N9R2VBC3D5EFGHJ',
          projectId: projectId,
          stage: JobStage.ingest,
          status: JobStatus.succeeded,
          progress: 1,
          attempt: 1,
          createdAt: now,
          updatedAt: now,
        ),
        Job(
          id: '01JZ8YKQ4T6N9R2VBC3D5EFGHJ',
          projectId: projectId,
          stage: JobStage.analyze,
          status: JobStatus.running,
          progress: 0.6,
          attempt: 1,
          createdAt: now,
          updatedAt: now,
        ),
      ],
    );
  }
}

void main() {
  testWidgets('shows every pipeline stage and live connection state', (
    tester,
  ) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          jobsControllerProvider.overrideWith(_FixedJobsController.new),
        ],
        child: const MaterialApp(home: JobPage(projectId: projectId)),
      ),
    );
    await tester.pump();

    for (final stage in JobStage.values) {
      expect(find.byKey(ValueKey('stage-${stage.name}')), findsOneWidget);
    }
    expect(find.text('Live'), findsOneWidget);
    expect(find.text('Running'), findsOneWidget);
    expect(find.byType(LinearProgressIndicator), findsOneWidget);
  });
}
