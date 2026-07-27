package com.gofront.app;

import android.app.Activity;
import android.content.res.AssetManager;
import android.graphics.Color;
import android.os.Build;
import android.os.Bundle;
import android.view.View;
import android.view.ViewGroup;
import android.view.Window;
import android.view.WindowManager;
import android.webkit.WebView;
import android.webkit.WebViewClient;

import java.io.File;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.Socket;

/**
 * Copies assets/frontend, starts libserver.so, loads http://127.0.0.1:8080 in a WebView.
 */
public class MainActivity extends Activity {

    private static final String ADDR = "127.0.0.1";
    private static final int PORT = 8080;

    private WebView web;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        requestWindowFeature(Window.FEATURE_NO_TITLE);
        super.onCreate(savedInstanceState);
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_DRAWS_SYSTEM_BAR_BACKGROUNDS);
        getWindow().clearFlags(WindowManager.LayoutParams.FLAG_TRANSLUCENT_STATUS);
        getWindow().setStatusBarColor(Color.TRANSPARENT);
        getWindow().setNavigationBarColor(Color.TRANSPARENT);
        if (Build.VERSION.SDK_INT >= 28) {
            WindowManager.LayoutParams lp = getWindow().getAttributes();
            lp.layoutInDisplayCutoutMode =
                    WindowManager.LayoutParams.LAYOUT_IN_DISPLAY_CUTOUT_MODE_SHORT_EDGES;
            getWindow().setAttributes(lp);
        }
        hideSystemUI();

        web = new WebView(this);
        web.getSettings().setJavaScriptEnabled(true);
        web.getSettings().setDomStorageEnabled(true);
        web.setWebViewClient(new WebViewClient());
        setContentView(web, new ViewGroup.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.MATCH_PARENT));

        final File frontend = new File(getFilesDir(), "frontend");
        try {
            copyAssetDir("frontend", frontend);
        } catch (IOException e) {
            // optional assets
        }

        startServer(frontend.getAbsolutePath());
        waitThenLoad();
    }

    @Override
    public void onWindowFocusChanged(boolean hasFocus) {
        super.onWindowFocusChanged(hasFocus);
        if (hasFocus) {
            hideSystemUI();
        }
    }

    private void hideSystemUI() {
        getWindow().getDecorView().setSystemUiVisibility(
                View.SYSTEM_UI_FLAG_LAYOUT_STABLE
                        | View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN
                        | View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION
                        | View.SYSTEM_UI_FLAG_FULLSCREEN
                        | View.SYSTEM_UI_FLAG_HIDE_NAVIGATION
                        | View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY);
    }

    private void startServer(String frontendDir) {
        try {
            String bin = getApplicationInfo().nativeLibraryDir + "/libserver.so";
            ProcessBuilder pb = new ProcessBuilder(
                    bin,
                    "-addr", ADDR + ":" + PORT,
                    "-frontend", frontendDir);
            pb.redirectErrorStream(true);
            pb.start();
        } catch (Exception e) {
            final String msg = "failed to start server: " + e;
            runOnUiThread(new Runnable() {
                public void run() {
                    web.loadData("<h1>GoFront</h1><pre>" + msg + "</pre>",
                            "text/html", "utf-8");
                }
            });
        }
    }

    private void waitThenLoad() {
        new Thread(new Runnable() {
            public void run() {
                for (int i = 0; i < 100; i++) {
                    Socket s = null;
                    try {
                        s = new Socket(ADDR, PORT);
                        break;
                    } catch (IOException e) {
                        try { Thread.sleep(100); } catch (InterruptedException ignored) {}
                    } finally {
                        if (s != null) { try { s.close(); } catch (IOException ignored) {} }
                    }
                }
                runOnUiThread(new Runnable() {
                    public void run() {
                        web.loadUrl("http://" + ADDR + ":" + PORT + "/");
                    }
                });
            }
        }).start();
    }

    private void copyAssetDir(String assetPath, File dst) throws IOException {
        AssetManager am = getAssets();
        String[] children = am.list(assetPath);
        if (children == null || children.length == 0) {
            copyAssetFile(am, assetPath, dst);
            return;
        }
        if (!dst.exists()) dst.mkdirs();
        for (String child : children) {
            copyAssetDir(assetPath + "/" + child, new File(dst, child));
        }
    }

    private void copyAssetFile(AssetManager am, String assetPath, File dst) throws IOException {
        File parent = dst.getParentFile();
        if (parent != null && !parent.exists()) parent.mkdirs();
        InputStream in = am.open(assetPath);
        OutputStream out = new FileOutputStream(dst);
        byte[] buf = new byte[8192];
        int n;
        while ((n = in.read(buf)) > 0) out.write(buf, 0, n);
        out.flush();
        out.close();
        in.close();
    }
}
