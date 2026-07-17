import 'package:web/web.dart' as web;

Future<void> saveDownload(String url, String _) async {
  final uri = Uri.tryParse(url);
  if (uri == null || (uri.scheme != 'http' && uri.scheme != 'https')) {
    throw const FormatException('Invalid download URL.');
  }

  // url_launcher uses window.open, which browsers may reject after awaiting
  // the API call that mints this URL. A same-tab navigation is not a popup and
  // the signed response's Content-Disposition makes it a file download.
  web.window.location.assign(uri.toString());
}
