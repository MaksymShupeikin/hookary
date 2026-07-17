import '../../core/services/api_client.dart';
import 'job_models.dart';

class JobsApi {
  JobsApi(this._client);

  final ApiClient _client;

  Future<JobList> listProject(String projectId) async {
    final json = await _client.get<Map<String, dynamic>>(
      '/v1/projects/$projectId/jobs',
    );
    return JobList.fromJson(json);
  }

  Future<Job> get(String jobId) async {
    final json = await _client.get<Map<String, dynamic>>('/v1/jobs/$jobId');
    return Job.fromJson(json);
  }

  Future<Job> retry(String jobId) async {
    final json = await _client.post<Map<String, dynamic>>(
      '/v1/jobs/$jobId/retry',
    );
    return Job.fromJson(json);
  }
}
