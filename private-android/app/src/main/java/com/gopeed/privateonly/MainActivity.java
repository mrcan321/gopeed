package com.gopeed.privateonly;

import android.Manifest;
import android.app.Activity;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.graphics.Typeface;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.text.InputType;
import android.view.ViewGroup;
import android.view.WindowManager;
import android.widget.Button;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;
import android.widget.Toast;

import java.net.URI;

/**
 * No Flutter, browser, WebView, sharing, storage picker, telemetry, update checker,
 * embedded API, extension loader, BitTorrent or JavaScript engine.
 * All network activity is in DownloadService and requires a tap on Download.
 */
public final class MainActivity extends Activity {
    private final Handler handler = new Handler(Looper.getMainLooper());
    private EditText urlInput;
    private TextView statusView;
    private Button startButton;
    private Button cancelButton;

    private final Runnable refresh = new Runnable() {
        @Override public void run() {
            statusView.setText(DownloadService.status);
            startButton.setEnabled(!DownloadService.running);
            cancelButton.setEnabled(DownloadService.running);
            handler.postDelayed(this, 750);
        }
    };

    @Override public void onCreate(Bundle state) {
        super.onCreate(state);
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_SECURE);

        int pad = (int) (22 * getResources().getDisplayMetrics().density);
        LinearLayout layout = new LinearLayout(this);
        layout.setOrientation(LinearLayout.VERTICAL);
        layout.setPadding(pad, pad, pad, pad);

        TextView title = new TextView(this);
        title.setText("Gopeed Gizli İndirici");
        title.setTextSize(23);
        title.setTypeface(null, Typeface.BOLD);
        layout.addView(title);

        TextView details = new TextView(this);
        details.setText("Yalnızca sizin girdiğiniz HTTPS adresinden dosya indirir. Dosyalar telefonun Download klasörüne kaydedilir. Analitik, güncelleme, torrent, eklenti, web sunucusu ve otomatik dış bağlantı yoktur. Yönlendirmeler güvenlik için reddedilir.");
        details.setTextSize(15);
        details.setPadding(0, pad / 2, 0, pad);
        layout.addView(details);

        urlInput = new EditText(this);
        urlInput.setHint("https://ornek.com/dosya.zip");
        urlInput.setSingleLine(false);
        urlInput.setMinLines(2);
        urlInput.setMaxLines(4);
        urlInput.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_URI);
        layout.addView(urlInput, new LinearLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));

        startButton = new Button(this);
        startButton.setText("Download klasörüne indir");
        startButton.setOnClickListener(v -> startDownload());
        layout.addView(startButton);

        cancelButton = new Button(this);
        cancelButton.setText("İndirmeyi iptal et");
        cancelButton.setOnClickListener(v -> {
            Intent cancel = new Intent(this, DownloadService.class);
            cancel.setAction(DownloadService.ACTION_CANCEL);
            startService(cancel);
        });
        layout.addView(cancelButton);

        statusView = new TextView(this);
        statusView.setTextSize(16);
        statusView.setPadding(0, pad / 2, 0, 0);
        layout.addView(statusView);

        ScrollView scroll = new ScrollView(this);
        scroll.addView(layout);
        setContentView(scroll);
    }

    private void startDownload() {
        String input = urlInput.getText().toString().trim();
        try {
            if (input.length() == 0 || input.length() > 8192) {
                throw new IllegalArgumentException("Geçersiz adres uzunluğu");
            }
            URI uri = new URI(input);
            if (!"https".equalsIgnoreCase(uri.getScheme()) ||
                    uri.getHost() == null || uri.getHost().isEmpty() ||
                    uri.getRawUserInfo() != null || uri.getRawFragment() != null) {
                throw new IllegalArgumentException("Sadece geçerli HTTPS dosya adresi girin");
            }
            if (DownloadService.running) {
                Toast.makeText(this, "Önce mevcut indirmeyi bitirin", Toast.LENGTH_SHORT).show();
                return;
            }
            Intent intent = new Intent(this, DownloadService.class);
            intent.setAction(DownloadService.ACTION_START);
            intent.putExtra(DownloadService.EXTRA_URL, uri.toASCIIString());
            // This is a foreground service started by an explicit user tap.
            startForegroundService(intent);
            if (Build.VERSION.SDK_INT >= 33 &&
                    checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS)
                        != PackageManager.PERMISSION_GRANTED) {
                requestPermissions(new String[]{Manifest.permission.POST_NOTIFICATIONS}, 100);
            }
        } catch (Exception e) {
            Toast.makeText(this, "Hatalı adres: " + e.getMessage(), Toast.LENGTH_LONG).show();
        }
    }

    @Override protected void onResume() {
        super.onResume();
        handler.post(refresh);
    }

    @Override protected void onPause() {
        handler.removeCallbacks(refresh);
        super.onPause();
    }
}
