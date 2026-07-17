import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/exceptions/exceptions.dart';
import '../../core/services/websocket_client.dart';
import '../auth/auth_controller.dart';
import '../projects/projects_controller.dart';
import 'job_models.dart';
import 'jobs_api.dart';

final jobsApiProvider = Provider<JobsApi>((ref) {
  return JobsApi(ref.watch(apiClientProvider));
});

final jobsControllerProvider = NotifierProvider<JobsController, JobsState>(
  JobsController.new,
);

class JobsController extends Notifier<JobsState> {
  Timer? _pollTimer;
  Timer? _reconnectTimer;
  ProjectJobSocket? _socket;
  StreamSubscription<Map<String, dynamic>>? _socketSubscription;
  int _generation = 0;

  @override
  JobsState build() {
    ref.onDispose(_stop);
    return const JobsState();
  }

  JobsApi get _api => ref.read(jobsApiProvider);

  Future<void> watchProject(String projectId, {bool refresh = false}) async {
    if (!refresh && state.projectId == projectId && state.hasLoaded) return;
    final switched = state.projectId != projectId;
    if (switched) {
      await _disconnect();
      _generation++;
      state = JobsState(projectId: projectId, isLoading: true);
    } else {
      state = state.copyWith(isLoading: true, clearError: true);
    }

    final generation = _generation;
    try {
      final result = await _api.listProject(projectId);
      if (generation != _generation || state.projectId != projectId) return;
      state = state.copyWith(
        jobs: _sorted(result.jobs),
        isLoading: false,
        hasLoaded: true,
        clearError: true,
      );
      _startPolling(projectId, generation);
      await _connect(projectId, generation);
    } on ApiException catch (error) {
      if (generation != _generation) return;
      state = state.copyWith(
        isLoading: false,
        hasLoaded: true,
        error: error.message,
      );
      _startPolling(projectId, generation);
    }
  }

  Future<void> refresh() async {
    final projectId = state.projectId;
    if (projectId == null) return;
    try {
      final result = await _api.listProject(projectId);
      if (state.projectId != projectId) return;
      state = state.copyWith(
        jobs: _sorted(result.jobs),
        isLoading: false,
        hasLoaded: true,
        clearError: true,
      );
    } on ApiException catch (error) {
      if (!state.hasLoaded) state = state.copyWith(error: error.message);
    }
  }

  Future<void> retry(String jobId) async {
    if (state.retryingJobId != null) return;
    state = state.copyWith(retryingJobId: jobId, clearError: true);
    try {
      final job = await _api.retry(jobId);
      _upsert(job);
    } on ApiException catch (error) {
      state = state.copyWith(error: error.message);
    } finally {
      state = state.copyWith(clearRetrying: true);
    }
  }

  void _startPolling(String projectId, int generation) {
    _pollTimer?.cancel();
    _pollTimer = Timer.periodic(const Duration(seconds: 5), (_) {
      if (state.projectId == projectId && generation == _generation) {
        unawaited(refresh());
        if (!state.isLive && _reconnectTimer == null) {
          _scheduleReconnect(projectId, generation);
        }
      }
    });
  }

  Future<void> _connect(String projectId, int generation) async {
    if (_socket != null || generation != _generation) return;
    final token = ref.read(tokenStorageProvider).accessToken;
    if (token == null) {
      _scheduleReconnect(projectId, generation);
      return;
    }
    try {
      final socket = await ProjectJobSocket.connect(
        accessToken: token,
        projectId: projectId,
      );
      if (generation != _generation || state.projectId != projectId) {
        await socket.close();
        return;
      }
      _socket = socket;
      state = state.copyWith(isLive: true);
      _socketSubscription = socket.messages.listen(
        _onSocketMessage,
        onError: (_) => _socketClosed(projectId, generation),
        onDone: () => _socketClosed(projectId, generation),
      );
    } on Object {
      _scheduleReconnect(projectId, generation);
    }
  }

  void _onSocketMessage(Map<String, dynamic> message) {
    switch (message['type']) {
      case 'snapshot':
        final jobs = message['jobs'] as List<dynamic>? ?? const [];
        state = state.copyWith(
          jobs: _sorted(
            jobs
                .map((item) => Job.fromJson(item as Map<String, dynamic>))
                .toList(),
          ),
          hasLoaded: true,
          isLoading: false,
        );
      case 'job_updated':
        final value = message['job'];
        if (value is Map<String, dynamic>) _upsert(Job.fromJson(value));
    }
  }

  void _upsert(Job job) {
    if (job.projectId != state.projectId) return;
    final jobs = [...state.jobs];
    final index = jobs.indexWhere((current) => current.id == job.id);
    if (index == -1) {
      jobs.add(job);
    } else {
      jobs[index] = job;
    }
    state = state.copyWith(jobs: _sorted(jobs), clearError: true);
    if (job.status == JobStatus.failed ||
        (job.stage == JobStage.publish && job.status == JobStatus.succeeded)) {
      unawaited(
        ref.read(projectsControllerProvider.notifier).load(refresh: true),
      );
    }
  }

  void _socketClosed(String projectId, int generation) {
    if (generation != _generation) return;
    _socketSubscription = null;
    _socket = null;
    state = state.copyWith(isLive: false);
    _scheduleReconnect(projectId, generation);
  }

  void _scheduleReconnect(String projectId, int generation) {
    if (_reconnectTimer != null || generation != _generation) return;
    _reconnectTimer = Timer(const Duration(seconds: 2), () {
      _reconnectTimer = null;
      if (state.projectId == projectId && generation == _generation) {
        unawaited(_connect(projectId, generation));
      }
    });
  }

  Future<void> _disconnect() async {
    _pollTimer?.cancel();
    _pollTimer = null;
    _reconnectTimer?.cancel();
    _reconnectTimer = null;
    await _socketSubscription?.cancel();
    _socketSubscription = null;
    await _socket?.close();
    _socket = null;
  }

  void _stop() {
    _generation++;
    unawaited(_disconnect());
  }

  List<Job> _sorted(List<Job> jobs) {
    final sorted = [...jobs];
    sorted.sort((a, b) {
      final stage = a.stage.index.compareTo(b.stage.index);
      return stage != 0 ? stage : a.createdAt.compareTo(b.createdAt);
    });
    return sorted;
  }
}
