package com.gofront.app;

import android.app.Activity;
import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.content.Context;
import android.os.Build;
import android.util.Log;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.ServerSocket;
import java.net.Socket;
import java.nio.charset.Charset;
import java.util.concurrent.atomic.AtomicInteger;

/**
 * Localhost HTTP bridge so the Go server can call Android APIs
 * (POST http://127.0.0.1:8081/gofront/android).
 */
final class AndroidBridge {

    static final String ADDR = "127.0.0.1";
    static final int PORT = 8081;
    static final String ENV_ADDR = "GOFRONT_ANDROID_ADDR";
    static final String PATH = "/gofront/android";
    static final String CHANNEL_ID = "gofront";
    private static final String TAG = "gofront-android";

    private final Activity activity;
    private final AtomicInteger notifyId = new AtomicInteger(1000);
    private ServerSocket server;
    private Thread thread;

    AndroidBridge(Activity activity) {
        this.activity = activity;
    }

    void start() {
        thread = new Thread(new Runnable() {
            public void run() {
                try {
                    server = new ServerSocket(PORT, 8, InetAddress.getByName(ADDR));
                    Log.i(TAG, "listening on " + ADDR + ":" + PORT);
                    while (!server.isClosed()) {
                        Socket sock = server.accept();
                        handleClient(sock);
                    }
                } catch (IOException e) {
                    Log.e(TAG, "bridge stopped: " + e);
                }
            }
        }, "gofront-android-bridge");
        thread.setDaemon(true);
        thread.start();
        ensureChannel();
    }

    void stop() {
        if (server != null) {
            try {
                server.close();
            } catch (IOException ignored) {
            }
        }
    }

    private void ensureChannel() {
        if (Build.VERSION.SDK_INT < 26) {
            return;
        }
        NotificationManager nm =
                (NotificationManager) activity.getSystemService(Context.NOTIFICATION_SERVICE);
        if (nm == null) {
            return;
        }
        NotificationChannel ch = new NotificationChannel(
                CHANNEL_ID, "GoFront", NotificationManager.IMPORTANCE_DEFAULT);
        nm.createNotificationChannel(ch);
    }

    private void handleClient(Socket sock) {
        try {
            BufferedReader in = new BufferedReader(
                    new InputStreamReader(sock.getInputStream(), Charset.forName("UTF-8")));
            String requestLine = in.readLine();
            if (requestLine == null) {
                sock.close();
                return;
            }
            int contentLength = 0;
            String line;
            while ((line = in.readLine()) != null && line.length() > 0) {
                if (line.regionMatches(true, 0, "Content-Length:", 0, 15)) {
                    contentLength = Integer.parseInt(line.substring(15).trim());
                }
            }
            char[] bodyBuf = new char[Math.max(contentLength, 0)];
            int got = 0;
            while (got < contentLength) {
                int n = in.read(bodyBuf, got, contentLength - got);
                if (n < 0) {
                    break;
                }
                got += n;
            }
            String body = new String(bodyBuf, 0, got);

            String method = "";
            String path = "";
            String[] parts = requestLine.split(" ");
            if (parts.length >= 2) {
                method = parts[0];
                path = parts[1];
            }

            String responseJson;
            int status = 200;
            if (!"POST".equals(method) || !PATH.equals(path)) {
                status = 404;
                responseJson = "{\"error\":\"not found\"}";
            } else {
                responseJson = dispatch(body);
                if (responseJson.indexOf("\"error\"") >= 0 && responseJson.indexOf("\"ok\"") < 0) {
                    // keep 200 with error field for Go client; only protocol errors use 4xx
                }
            }
            writeResponse(sock, status, responseJson);
        } catch (Exception e) {
            try {
                writeResponse(sock, 500, jsonError(e.getMessage()));
            } catch (IOException ignored) {
            }
        } finally {
            try {
                sock.close();
            } catch (IOException ignored) {
            }
        }
    }

    private String dispatch(String body) {
        try {
            JSONObject req = new JSONObject(body);
            String method = req.getString("method");
            JSONArray args = req.optJSONArray("args");
            if (args == null) {
                args = new JSONArray();
            }
            if ("Notify".equals(method)) {
                String title = args.length() > 0 ? args.getString(0) : "";
                String text = args.length() > 1 ? args.getString(1) : "";
                return showNotification(title, text);
            }
            return jsonError("unknown method " + method);
        } catch (Exception e) {
            return jsonError(e.getMessage());
        }
    }

    private String showNotification(final String title, final String text) {
        final int id = notifyId.getAndIncrement();
        final Object lock = new Object();
        final String[] err = new String[1];
        synchronized (lock) {
            activity.runOnUiThread(new Runnable() {
                public void run() {
                    try {
                        NotificationManager nm = (NotificationManager)
                                activity.getSystemService(Context.NOTIFICATION_SERVICE);
                        if (nm == null) {
                            err[0] = "NotificationManager unavailable";
                        } else {
                            Notification.Builder b;
                            if (Build.VERSION.SDK_INT >= 26) {
                                b = new Notification.Builder(activity, CHANNEL_ID);
                            } else {
                                b = new Notification.Builder(activity);
                            }
                            b.setContentTitle(title)
                                    .setContentText(text)
                                    .setSmallIcon(android.R.drawable.ic_dialog_info)
                                    .setAutoCancel(true);
                            if (Build.VERSION.SDK_INT < 26) {
                                b.setPriority(Notification.PRIORITY_DEFAULT);
                            }
                            nm.notify(id, b.build());
                            Log.i(TAG, "Notify id=" + id + " title=" + title);
                        }
                    } catch (Exception e) {
                        err[0] = e.getMessage();
                        Log.e(TAG, "Notify failed", e);
                    }
                    synchronized (lock) {
                        lock.notify();
                    }
                }
            });
            try {
                lock.wait(3000);
            } catch (InterruptedException ignored) {
            }
        }
        if (err[0] != null) {
            return jsonError(err[0]);
        }
        return "{\"ok\":true}";
    }

    private static String jsonError(String msg) {
        if (msg == null) {
            msg = "error";
        }
        return "{\"error\":" + JSONObject.quote(msg) + "}";
    }

    private static void writeResponse(Socket sock, int status, String json) throws IOException {
        byte[] body = json.getBytes("UTF-8");
        String reason = status == 200 ? "OK" : "Error";
        StringBuilder sb = new StringBuilder();
        sb.append("HTTP/1.1 ").append(status).append(' ').append(reason).append("\r\n");
        sb.append("Content-Type: application/json; charset=utf-8\r\n");
        sb.append("Content-Length: ").append(body.length).append("\r\n");
        sb.append("Connection: close\r\n\r\n");
        OutputStream out = sock.getOutputStream();
        out.write(sb.toString().getBytes("UTF-8"));
        out.write(body);
        out.flush();
    }
}
