package com.gopeed.privateonly;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.ContentValues;
import android.content.Intent;
import android.content.pm.ServiceInfo;
import android.net.Uri;
import android.os.Build;
import android.os.Environment;
import android.os.IBinder;
import android.os.SystemClock;
import android.provider.MediaStore;

import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.Proxy;
import java.net.URI;
import java.net.URL;
import java.net.URLConnection;
import java.net.URLDecoder;
import java.nio.charset.StandardCharsets;
import java.util.Locale;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

import javax.net.ssl.HttpsURLConnection;

/**
 * Only network entrypoint in the shipped APK:
 *   one explicit HTTPS GET to the exact host supplied by the user.
 *
 * - No analytics, update/version check, cookies, HTTP proxy, upload, webhooks,
 *   automatic redirects, DNS prefetching, torrent/peer traffic or retries.
 * - No file reads from device storage. Writes only a newly created MediaStore
 *   item in primary shared-storage Download directory; no storage permission.
 * - Android itself still performs DNS and necessary HTTPS/TLS protocol exchange
 *   for an explicitly requested download.
 */
public final class DownloadService extends Service {
    public static final String ACTION_START = "com.gopeed.privateonly.START";
    public static final String ACTION_CANCEL = "com.gopeed.privateonly.CANCEL";
    public static final String EXTRA_URL = "url";

    public static volatile boolean running = false;
    public static volatile String status = "Hazır. HTTPS adresini girin.";
    private static final String CHANNEL = "user_downloads";
    private static final int NOTIFICATION_ID = 43;
    private static final Pattern FILENAME = Pattern.compile("filename\\s*=\\s*[\"']?([^\"';]+)", Pattern.CASE_INSENSITIVE);
    private static final Pattern UTF8_FILENAME = Pattern.compile("filename\\*\\s*=\\s*(?:UTF-8''|utf-8'')?([^;]+)", Pattern.CASE_INSENSITIVE);

    private volatile HttpsURLConnection activeConnection;
    private volatile Thread worker;
    private volatile boolean cancelled;

    @Override public void onCreate() {
        super.onCreate();
        NotificationManager manager = getSystemService(NotificationManager.class);
        manager.createNotificationChannel(new NotificationChannel(
            CHANNEL, "İndirmeler", NotificationManager.IMPORTANCE_LOW));
    }

    @Override public int onStartCommand(Intent intent, int flags, int startId) {
        if (intent == null) return START_NOT_STICKY;
        if (ACTION_CANCEL.equals(intent.getAction())) {
            cancelled = true;
            HttpsURLConnection connection = activeConnection;
            if (connection != null) connection.disconnect();
            Thread thread = worker;
            if (thread != null) thread.interrupt();
            return START_NOT_STICKY;
        }
        if (!ACTION_START.equals(intent.getAction()) || running) return START_NOT_STICKY;
        String suppliedUrl = intent.getStringExtra(EXTRA_URL);
        // Revalidate inside the service so that the network rules are not
        // dependent on how the activity validates its own input.
        URI uri;
        try {
            uri = new URI(suppliedUrl == null ? "" : suppliedUrl);
            if (suppliedUrl.length() > 8192 ||
                    !"https".equalsIgnoreCase(uri.getScheme()) ||
                    uri.getHost() == null || uri.getHost().isEmpty() ||
                    uri.getRawUserInfo() != null || uri.getRawFragment() != null) {
                throw new IllegalArgumentException("HTTPS adresi gerekli");
            }
        } catch (Exception e) {
            status = "Hatalı HTTPS adresi";
            stopSelf(startId);
            return START_NOT_STICKY;
        }

        cancelled = false;
        running = true;
        status = "Bağlanıyor...";
        Notification notice = notification("İndirme başlatılıyor", 0, 0, true);
        if (Build.VERSION.SDK_INT >= 29) {
            startForeground(NOTIFICATION_ID, notice, ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC);
        } else {
            startForeground(NOTIFICATION_ID, notice);
        }
        final URI requested = uri;
        worker = new Thread(() -> download(requested, startId), "user-requested-download");
        worker.start();
        return START_NOT_STICKY;  // never auto-restart after a process kill
    }

    private void download(URI requested, int startId) {
        Uri outputUri = null;
        boolean completed = false;
        String finalName = "";
        try {
            // NO_PROXY prevents device-configured HTTP proxy detours. The ONLY
            // destination is the URL host the user explicitly typed.
            URL url = requested.toURL();
            URLConnection raw = url.openConnection(Proxy.NO_PROXY);
            if (!(raw instanceof HttpsURLConnection)) {
                throw new IOException("HTTPS gerekli");
            }
            HttpsURLConnection connection = (HttpsURLConnection) raw;
            activeConnection = connection;
            connection.setRequestMethod("GET");
            connection.setInstanceFollowRedirects(false);
            connection.setUseCaches(false);
            connection.setDefaultUseCaches(false);
            connection.setAllowUserInteraction(false);
            connection.setConnectTimeout(15000);
            connection.setReadTimeout(30000);
            connection.setRequestProperty("Accept-Encoding", "identity");
            // Avoid leaking the device model/Android version in the default user agent.
            connection.setRequestProperty("User-Agent", "PrivateDownloader/1.0");

            int code = connection.getResponseCode();
            if (cancelled) throw new IOException("İptal edildi");
            if (code >= 300 && code < 400) {
                throw new IOException("Adres başka sunucuya yönlendiriyor; otomatik yönlendirme engellendi (HTTP " + code + ")");
            }
            if (code != HttpURLConnection.HTTP_OK) {
                throw new IOException("Sunucu HTTP " + code + " yanıtını verdi");
            }

            finalName = safeFileName(connection.getHeaderField("Content-Disposition"), requested);
            ContentValues values = new ContentValues();
            values.put(MediaStore.MediaColumns.DISPLAY_NAME, finalName);
            values.put(MediaStore.MediaColumns.RELATIVE_PATH, Environment.DIRECTORY_DOWNLOADS + "/");
            String mime = URLConnection.guessContentTypeFromName(finalName);
            values.put(MediaStore.MediaColumns.MIME_TYPE,
                mime == null ? "application/octet-stream" : mime);
            values.put(MediaStore.MediaColumns.IS_PENDING, 1);
            outputUri = getContentResolver().insert(
                MediaStore.Downloads.getContentUri(MediaStore.VOLUME_EXTERNAL_PRIMARY), values);
            if (outputUri == null) throw new IOException("Download dizininde dosya oluşturulamadı");

            long size = connection.getContentLengthLong();
            long received = 0;
            long lastNotice = 0;
            try (InputStream in = connection.getInputStream();
                 OutputStream out = getContentResolver().openOutputStream(outputUri, "w")) {
                if (out == null) throw new IOException("Download yazma erişimi yok");
                byte[] buffer = new byte[64 * 1024];
                int count;
                while ((count = in.read(buffer)) != -1) {
                    if (cancelled || Thread.currentThread().isInterrupted()) {
                        throw new IOException("İptal edildi");
                    }
                    out.write(buffer, 0, count);
                    received += count;
                    long now = SystemClock.elapsedRealtime();
                    if (now - lastNotice > 800) {
                        int percent = size > 0 ? (int) Math.min(100L, received * 100L / size) : -1;
                        status = size > 0
                            ? String.format(Locale.ROOT, "İndiriliyor: %d%% (%d / %d MB)",
                                  percent, received / 1048576, size / 1048576)
                            : String.format(Locale.ROOT, "İndiriliyor: %d MB", received / 1048576);
                        getSystemService(NotificationManager.class).notify(
                            NOTIFICATION_ID,
                            notification("Dosya indiriliyor", percent, 100, size <= 0));
                        lastNotice = now;
                    }
                }
                out.flush();
            }
            if (cancelled) throw new IOException("İptal edildi");
            values.clear();
            values.put(MediaStore.MediaColumns.IS_PENDING, 0);
            if (getContentResolver().update(outputUri, values, null, null) != 1) {
                throw new IOException("Dosya kaydedilemedi");
            }
            completed = true;
            status = "İndirildi: " + finalName + " (Download)";
        } catch (Exception e) {
            // Do not expose the requested URL, credentials or file contents in logs.
            status = cancelled ? "İndirme iptal edildi" : "İndirme başarısız: " + e.getMessage();
        } finally {
            HttpsURLConnection c = activeConnection;
            if (c != null) c.disconnect();
            activeConnection = null;
            if (!completed && outputUri != null) {
                try {
                    getContentResolver().delete(outputUri, null, null);
                } catch (Exception ignored) {
                    // System may already have removed the unfinished file.
                }
            }
            running = false;
            worker = null;
            stopForeground(STOP_FOREGROUND_REMOVE);
            // All user-visible completion information is local to the device.
            getSystemService(NotificationManager.class).notify(
                NOTIFICATION_ID + 1,
                notification(completed ? "Dosya Download klasörüne kaydedildi" : "İndirme sonlandı", 0, 0, true));
            stopSelf(startId);
        }
    }

    private static String safeFileName(String disposition, URI request) {
        String name = null;
        if (disposition != null) {
            Matcher encoded = UTF8_FILENAME.matcher(disposition);
            if (encoded.find()) {
                try {
                    name = URLDecoder.decode(encoded.group(1).replace("\"", "").trim(),
                        StandardCharsets.UTF_8.name());
                } catch (Exception ignored) {}
            }
            if (name == null) {
                Matcher plain = FILENAME.matcher(disposition);
                if (plain.find()) name = plain.group(1).trim();
            }
        }
        if (name == null || name.isEmpty()) {
            String path = request.getRawPath();
            if (path != null && path.length() > 1) {
                String raw = path.substring(path.lastIndexOf('/') + 1);
                try {
                    name = URLDecoder.decode(raw.replace("+", "%2B"), StandardCharsets.UTF_8.name());
                } catch (Exception ignored) { name = raw; }
            }
        }
        if (name == null) name = "";
        // Remove traversal, slashes, control characters and other dangerous
        // filename metacharacters. This never changes the output directory.
        name = name.replaceAll("[\\\\/\\p{Cntrl}:*?\"<>|]", "_").trim();
        while (name.startsWith(".")) name = name.substring(1);
        if (name.isEmpty() || name.equals("..")) name = "download-" + System.currentTimeMillis();
        if (name.length() > 120) name = name.substring(0, 120);
        return name;
    }

    private Notification notification(String text, int progress, int max, boolean indeterminate) {
        Intent show = new Intent(this, MainActivity.class);
        PendingIntent open = PendingIntent.getActivity(this, 0, show,
            PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);
        Notification.Builder builder = new Notification.Builder(this, CHANNEL)
            .setSmallIcon(android.R.drawable.stat_sys_download)
            .setContentTitle("Gopeed Gizli İndirici")
            .setContentText(text)
            .setContentIntent(open)
            .setOnlyAlertOnce(true);
        if (running) builder.setOngoing(true).setProgress(max, Math.max(0, progress), indeterminate);
        return builder.build();
    }

    @Override public IBinder onBind(Intent intent) { return null; }
}
