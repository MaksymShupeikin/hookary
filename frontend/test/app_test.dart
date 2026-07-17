import 'package:flutter_test/flutter_test.dart';
import 'package:hookary/core/config/environment.dart';

void main() {
  test('websocket URL follows the API host', () {
    // The two must never point at different hosts, so wsBaseUrl is derived
    // rather than configured.
    expect(Environment.wsBaseUrl, 'ws://localhost:8080');
  });
}
