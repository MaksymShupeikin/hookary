import 'package:flutter_test/flutter_test.dart';
import 'package:hookary/features/jobs/job_models.dart';

void main() {
  test('parses a running job update', () {
    final job = Job.fromJson({
      'id': '01JZ8XKQ4T6N9R2VBC3D5EFGHJ',
      'project_id': '01JZ8WKQ4T6N9R2VBC3D5EFGHJ',
      'stage': 'analyze',
      'status': 'running',
      'progress': 0.42,
      'attempt': 2,
      'error': null,
      'created_at': '2026-07-16T09:15:00Z',
      'updated_at': '2026-07-16T09:16:00Z',
    });

    expect(job.stage, JobStage.analyze);
    expect(job.status, JobStatus.running);
    expect(job.progress, 0.42);
    expect(job.attempt, 2);
    expect(job.isActive, isTrue);
  });

  test('keeps a terminal error from the API', () {
    final job = Job.fromJson({
      'id': '01JZ8XKQ4T6N9R2VBC3D5EFGHJ',
      'project_id': '01JZ8WKQ4T6N9R2VBC3D5EFGHJ',
      'stage': 'render',
      'status': 'failed',
      'progress': null,
      'attempt': 5,
      'error': {'code': 'worker_lost', 'message': 'Worker stopped.'},
      'created_at': '2026-07-16T09:15:00Z',
      'updated_at': '2026-07-16T09:20:00Z',
    });

    expect(job.error?.code, 'worker_lost');
    expect(job.error?.message, 'Worker stopped.');
    expect(job.isActive, isFalse);
  });
}
