enum ProjectStatus { empty, processing, ready, failed }

class Project {
  const Project({
    required this.id,
    required this.title,
    required this.status,
    required this.assetCount,
    required this.clipCount,
    required this.createdAt,
    required this.updatedAt,
  });

  factory Project.fromJson(Map<String, dynamic> json) {
    return Project(
      id: json['id'] as String,
      title: json['title'] as String,
      status: ProjectStatus.values.byName(json['status'] as String),
      assetCount: json['asset_count'] as int,
      clipCount: json['clip_count'] as int,
      createdAt: DateTime.parse(json['created_at'] as String),
      updatedAt: DateTime.parse(json['updated_at'] as String),
    );
  }

  final String id;
  final String title;
  final ProjectStatus status;
  final int assetCount;
  final int clipCount;
  final DateTime createdAt;
  final DateTime updatedAt;
}

class ProjectList {
  const ProjectList({required this.projects, this.nextCursor});

  factory ProjectList.fromJson(Map<String, dynamic> json) {
    final items = json['projects'] as List<dynamic>? ?? const [];
    return ProjectList(
      projects: items
          .map((item) => Project.fromJson(item as Map<String, dynamic>))
          .toList(growable: false),
      nextCursor: json['next_cursor'] as String?,
    );
  }

  final List<Project> projects;
  final String? nextCursor;
}

class ProjectsState {
  const ProjectsState({
    this.projects = const [],
    this.nextCursor,
    this.isLoading = false,
    this.isLoadingMore = false,
    this.isMutating = false,
    this.hasLoaded = false,
    this.error,
  });

  final List<Project> projects;
  final String? nextCursor;
  final bool isLoading;
  final bool isLoadingMore;
  final bool isMutating;
  final bool hasLoaded;
  final String? error;

  Project? byId(String id) {
    for (final project in projects) {
      if (project.id == id) return project;
    }
    return null;
  }

  ProjectsState copyWith({
    List<Project>? projects,
    String? nextCursor,
    bool clearNextCursor = false,
    bool? isLoading,
    bool? isLoadingMore,
    bool? isMutating,
    bool? hasLoaded,
    String? error,
    bool clearError = false,
  }) {
    return ProjectsState(
      projects: projects ?? this.projects,
      nextCursor: clearNextCursor ? null : nextCursor ?? this.nextCursor,
      isLoading: isLoading ?? this.isLoading,
      isLoadingMore: isLoadingMore ?? this.isLoadingMore,
      isMutating: isMutating ?? this.isMutating,
      hasLoaded: hasLoaded ?? this.hasLoaded,
      error: clearError ? null : error ?? this.error,
    );
  }
}
