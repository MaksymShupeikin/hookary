import 'dart:convert';

import 'package:web_socket_channel/web_socket_channel.dart';

import '../config/environment.dart';

class ProjectJobSocket {
  ProjectJobSocket._(this._channel);

  final WebSocketChannel _channel;

  static Future<ProjectJobSocket> connect({
    required String accessToken,
    required String projectId,
  }) async {
    final uri = Uri.parse('${Environment.wsBaseUrl}/v1/ws');
    final channel = WebSocketChannel.connect(uri);
    await channel.ready;
    channel.sink.add(
      jsonEncode({
        'type': 'authenticate',
        'access_token': accessToken,
        'project_id': projectId,
      }),
    );
    return ProjectJobSocket._(channel);
  }

  Stream<Map<String, dynamic>> get messages => _channel.stream.map((data) {
    if (data is! String) {
      throw const FormatException('The job stream sent a binary message.');
    }
    final decoded = jsonDecode(data);
    if (decoded is! Map<String, dynamic>) {
      throw const FormatException('The job stream sent an invalid message.');
    }
    return decoded;
  });

  Future<void> close() async {
    await _channel.sink.close();
  }
}
