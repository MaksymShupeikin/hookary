enum JobStage { ingest, analyze, render, publish }

enum JobStatus { queued, running, succeeded, failed }

class JobError {
  const JobError({required this.code, required this.message});

  factory JobError.fromJson(Map<String, dynamic> json) {
    return JobError(
      code: json['code'] as String,
      message: json['message'] as String? ?? 'The job failed.',
    );
  }

  final String code;
  final String message;
}

class Job {
  const Job({
    required this.id,
    required this.projectId,
    required this.stage,
    required this.status,
    required this.attempt,
    required this.createdAt,
    required this.updatedAt,
    this.progress,
    this.error,
  });

  factory Job.fromJson(Map<String, dynamic> json) {
    final error = json['error'];
    return Job(
      id: json['id'] as String,
      projectId: json['project_id'] as String,
      stage: JobStage.values.byName(json['stage'] as String),
      status: JobStatus.values.byName(json['status'] as String),
      progress: (json['progress'] as num?)?.toDouble(),
      attempt: json['attempt'] as int,
      error: error is Map<String, dynamic> ? JobError.fromJson(error) : null,
      createdAt: DateTime.parse(json['created_at'] as String),
      updatedAt: DateTime.parse(json['updated_at'] as String),
    );
  }

  final String id;
  final String projectId;
  final JobStage stage;
  final JobStatus status;
  final double? progress;
  final int attempt;
  final JobError? error;
  final DateTime createdAt;
  final DateTime updatedAt;

  bool get isActive =>
      status == JobStatus.queued || status == JobStatus.running;
}

class JobList {
  const JobList(this.jobs);

  factory JobList.fromJson(Map<String, dynamic> json) {
    final jobs = json['jobs'] as List<dynamic>? ?? const [];
    return JobList(
      jobs
          .map((item) => Job.fromJson(item as Map<String, dynamic>))
          .toList(growable: false),
    );
  }

  final List<Job> jobs;
}

class JobsState {
  const JobsState({
    this.projectId,
    this.jobs = const [],
    this.isLoading = false,
    this.hasLoaded = false,
    this.isLive = false,
    this.retryingJobId,
    this.error,
  });

  final String? projectId;
  final List<Job> jobs;
  final bool isLoading;
  final bool hasLoaded;
  final bool isLive;
  final String? retryingJobId;
  final String? error;

  bool get hasActiveJobs => jobs.any((job) => job.isActive);

  JobsState copyWith({
    String? projectId,
    List<Job>? jobs,
    bool? isLoading,
    bool? hasLoaded,
    bool? isLive,
    String? retryingJobId,
    bool clearRetrying = false,
    String? error,
    bool clearError = false,
  }) {
    return JobsState(
      projectId: projectId ?? this.projectId,
      jobs: jobs ?? this.jobs,
      isLoading: isLoading ?? this.isLoading,
      hasLoaded: hasLoaded ?? this.hasLoaded,
      isLive: isLive ?? this.isLive,
      retryingJobId: clearRetrying ? null : retryingJobId ?? this.retryingJobId,
      error: clearError ? null : error ?? this.error,
    );
  }
}
