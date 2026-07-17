import 'package:url_launcher/url_launcher.dart';

Future<void> saveDownload(String url, String _) async {
  final uri = Uri.tryParse(url);
  if (uri == null || !uri.hasScheme) {
    throw const FormatException('Invalid download URL.');
  }
  final opened = await launchUrl(uri, mode: LaunchMode.externalApplication);
  if (!opened) {
    throw StateError('No application could open the download URL.');
  }
}
