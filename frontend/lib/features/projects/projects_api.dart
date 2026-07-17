import '../../core/services/api_client.dart';
import 'project_models.dart';

class ProjectsApi {
  ProjectsApi(this._client);

  final ApiClient _client;

  Future<ProjectList> list({String? cursor, int limit = 20}) async {
    final json = await _client.get<Map<String, dynamic>>(
      '/v1/projects',
      query: {'limit': limit, 'cursor': ?cursor},
    );
    return ProjectList.fromJson(json);
  }

  Future<Project> get(String id) async {
    final json = await _client.get<Map<String, dynamic>>('/v1/projects/$id');
    return Project.fromJson(json);
  }

  Future<Project> create(String title) async {
    final json = await _client.post<Map<String, dynamic>>(
      '/v1/projects',
      body: {'title': title},
    );
    return Project.fromJson(json);
  }

  Future<void> delete(String id) => _client.delete<void>('/v1/projects/$id');
}
