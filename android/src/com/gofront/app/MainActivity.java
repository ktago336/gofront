package com.gofront.app;

import android.Manifest;
import android.app.Activity;
import android.content.ActivityNotFoundException;
import android.content.ContentValues;
import android.content.Intent;
import android.content.pm.PackageInfo;
import android.content.pm.PackageManager;
import android.content.res.AssetManager;
import android.graphics.Color;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.provider.MediaStore;
import android.view.View;
import android.view.ViewGroup;
import android.view.Window;
import android.view.WindowManager;
import android.webkit.PermissionRequest;
import android.webkit.ValueCallback;
import android.webkit.WebChromeClient;
import android.webkit.WebView;
import android.webkit.WebViewClient;

import java.io.File;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.Socket;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Set;

/**
 * Copies assets/frontend, starts libserver.so, loads http://127.0.0.1:8080 in a WebView.
 */
public class MainActivity extends Activity {

    private static final String ADDR = "127.0.0.1";
    private static final int PORT = 8080;
    private static final int REQ_WEB_PERMISSIONS = 1001;
    private static final int REQ_FILE_CHOOSER = 1002;

    private WebView web;
    private Set<String> declaredPermissions;
    private PermissionRequest pendingWebPermission;
    private ValueCallback<Uri[]> fileCallback;
    private Uri cameraOutputUri;

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

        declaredPermissions = loadDeclaredPermissions();

        web = new WebView(this);
        web.getSettings().setJavaScriptEnabled(true);
        web.getSettings().setDomStorageEnabled(true);
        web.setWebViewClient(new WebViewClient());
        web.setWebChromeClient(new WebChromeClient() {
            @Override
            public void onPermissionRequest(final PermissionRequest request) {
                runOnUiThread(new Runnable() {
                    public void run() {
                        handleWebPermissionRequest(request);
                    }
                });
            }

            @Override
            public boolean onShowFileChooser(WebView view, ValueCallback<Uri[]> callback,
                    FileChooserParams params) {
                return handleShowFileChooser(callback, params);
            }
        });
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

    @Override
    public void onRequestPermissionsResult(int requestCode, String[] permissions, int[] grantResults) {
        if (requestCode != REQ_WEB_PERMISSIONS) {
            super.onRequestPermissionsResult(requestCode, permissions, grantResults);
            return;
        }
        PermissionRequest req = pendingWebPermission;
        pendingWebPermission = null;
        if (req == null) {
            return;
        }
        boolean ok = grantResults.length > 0;
        for (int i = 0; i < grantResults.length; i++) {
            if (grantResults[i] != PackageManager.PERMISSION_GRANTED) {
                ok = false;
                break;
            }
        }
        if (ok) {
            req.grant(req.getResources());
        } else {
            req.deny();
        }
    }

    @Override
    protected void onActivityResult(int requestCode, int resultCode, Intent data) {
        if (requestCode != REQ_FILE_CHOOSER) {
            super.onActivityResult(requestCode, resultCode, data);
            return;
        }
        ValueCallback<Uri[]> cb = fileCallback;
        fileCallback = null;
        if (cb == null) {
            return;
        }
        Uri[] results = null;
        if (resultCode == RESULT_OK) {
            if (data != null && (data.getDataString() != null || data.getClipData() != null)) {
                results = WebChromeClient.FileChooserParams.parseResult(resultCode, data);
            } else if (cameraOutputUri != null) {
                results = new Uri[]{cameraOutputUri};
            }
        }
        cameraOutputUri = null;
        cb.onReceiveValue(results);
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

    private boolean handleShowFileChooser(ValueCallback<Uri[]> callback,
            WebChromeClient.FileChooserParams params) {
        if (fileCallback != null) {
            fileCallback.onReceiveValue(null);
        }
        fileCallback = callback;
        cameraOutputUri = null;

        Intent content = params.createIntent();
        Intent chooser = Intent.createChooser(content, null);

        if (shouldOfferCamera(params)) {
            Intent camera = tryCameraIntent();
            if (camera != null) {
                chooser.putExtra(Intent.EXTRA_INITIAL_INTENTS, new Intent[]{camera});
            }
        }

        try {
            startActivityForResult(chooser, REQ_FILE_CHOOSER);
            return true;
        } catch (ActivityNotFoundException e) {
            fileCallback = null;
            cameraOutputUri = null;
            return false;
        }
    }

    private boolean shouldOfferCamera(WebChromeClient.FileChooserParams params) {
        if (params.isCaptureEnabled()) {
            return true;
        }
        String[] types = params.getAcceptTypes();
        if (types == null || types.length == 0) {
            return false;
        }
        for (int i = 0; i < types.length; i++) {
            String t = types[i];
            if (t == null || t.length() == 0 || "*/*".equals(t)) {
                continue;
            }
            if (t.startsWith("image/") || "image/*".equals(t)) {
                return true;
            }
        }
        return false;
    }

    private Intent tryCameraIntent() {
        Intent take = new Intent(MediaStore.ACTION_IMAGE_CAPTURE);
        if (take.resolveActivity(getPackageManager()) == null) {
            return null;
        }
        try {
            ContentValues values = new ContentValues();
            values.put(MediaStore.Images.Media.MIME_TYPE, "image/jpeg");
            values.put(MediaStore.Images.Media.DISPLAY_NAME,
                    "gofront_" + System.currentTimeMillis() + ".jpg");
            Uri uri = getContentResolver().insert(
                    MediaStore.Images.Media.EXTERNAL_CONTENT_URI, values);
            if (uri == null) {
                return null;
            }
            cameraOutputUri = uri;
            take.putExtra(MediaStore.EXTRA_OUTPUT, uri);
            take.addFlags(Intent.FLAG_GRANT_WRITE_URI_PERMISSION
                    | Intent.FLAG_GRANT_READ_URI_PERMISSION);
            return take;
        } catch (Exception e) {
            cameraOutputUri = null;
            return null;
        }
    }

    private Set<String> loadDeclaredPermissions() {
        Set<String> out = new HashSet<String>();
        try {
            PackageInfo info = getPackageManager().getPackageInfo(
                    getPackageName(), PackageManager.GET_PERMISSIONS);
            if (info.requestedPermissions != null) {
                for (int i = 0; i < info.requestedPermissions.length; i++) {
                    out.add(info.requestedPermissions[i]);
                }
            }
        } catch (PackageManager.NameNotFoundException ignored) {
        }
        return out;
    }

    private void handleWebPermissionRequest(PermissionRequest request) {
        if (pendingWebPermission != null) {
            pendingWebPermission.deny();
            pendingWebPermission = null;
        }

        String[] resources = request.getResources();
        List<String> androidPerms = new ArrayList<String>();
        for (int i = 0; i < resources.length; i++) {
            String androidPerm = webResourceToAndroidPermission(resources[i]);
            if (androidPerm == null) {
                continue;
            }
            if (!declaredPermissions.contains(androidPerm)) {
                request.deny();
                return;
            }
            if (!androidPerms.contains(androidPerm)) {
                androidPerms.add(androidPerm);
            }
        }

        List<String> needAsk = new ArrayList<String>();
        if (Build.VERSION.SDK_INT >= 23) {
            for (int i = 0; i < androidPerms.size(); i++) {
                String p = androidPerms.get(i);
                if (checkSelfPermission(p) != PackageManager.PERMISSION_GRANTED) {
                    needAsk.add(p);
                }
            }
        }

        if (needAsk.isEmpty()) {
            request.grant(resources);
            return;
        }

        pendingWebPermission = request;
        requestPermissions(needAsk.toArray(new String[needAsk.size()]), REQ_WEB_PERMISSIONS);
    }

    private static String webResourceToAndroidPermission(String resource) {
        if (PermissionRequest.RESOURCE_VIDEO_CAPTURE.equals(resource)) {
            return Manifest.permission.CAMERA;
        }
        if (PermissionRequest.RESOURCE_AUDIO_CAPTURE.equals(resource)) {
            return Manifest.permission.RECORD_AUDIO;
        }
        return null;
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
