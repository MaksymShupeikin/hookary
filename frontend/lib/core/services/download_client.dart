import 'download_client_io.dart'
    if (dart.library.js_interop) 'download_client_web.dart'
    as platform;

/// Starts a direct download from object storage without proxying media bytes.
class DownloadClient {
  const DownloadClient();

  Future<void> save({required String url, required String filename}) {
    return platform.saveDownload(url, filename);
  }
}
