package com.gosshd.desktop.rpc;

import android.util.Base64;
import com.gosshd.desktop.access.AccessibilityBackend;
import com.gosshd.desktop.local.LocalState;
import java.io.*;
import java.net.*;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.SecureRandom;
import java.util.Set;
import java.util.concurrent.*;
import java.util.function.IntConsumer;
import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;
import org.json.JSONObject;

// Authenticated loopback only, locally enabled. No screenshot/tree/input logs.
public final class BridgeServer implements AutoCloseable {
  private final LocalState state;
  private final IntConsumer clientsChanged;
  private final Set<Socket> sockets = ConcurrentHashMap.newKeySet();
  private final ExecutorService workers = Executors.newFixedThreadPool(4);
  private final ServerSocket listener;
  private volatile boolean closed;

  public BridgeServer(LocalState state, IntConsumer changed) throws Exception {
    this.state = state;
    clientsChanged = changed;
    listener = new ServerSocket();
    listener.setReuseAddress(true);
    listener.bind(new InetSocketAddress(InetAddress.getByName("127.0.0.1"), 28765), 4);
    Thread accept =
        new Thread(
            () -> {
              while (!closed) {
                try {
                  Socket socket = listener.accept();
                  if (sockets.size() >= 4) {
                    socket.close();
                    continue;
                  }
                  sockets.add(socket);
                  workers.submit(() -> serve(socket));
                } catch (Exception e) {
                  if (!closed) close();
                }
              }
            },
            "desktop-loopback");
    accept.start();
  }

  private static String line(InputStream in, int max) throws Exception {
    ByteArrayOutputStream out = new ByteArrayOutputStream();
    for (int b; (b = in.read()) != -1; ) {
      if (b == '\n') return out.toString("UTF-8");
      if (out.size() >= max) throw new IOException("Request exceeds limit");
      out.write(b);
    }
    return null;
  }

  private static void write(OutputStream out, JSONObject value) throws Exception {
    byte[] bytes = (value.toString() + "\n").getBytes(StandardCharsets.UTF_8);
    if (bytes.length > 16 * 1024 * 1024) throw new IOException("Response exceeds limit");
    out.write(bytes);
    out.flush();
  }

  private void serve(Socket socket) {
    boolean authenticated = false;
    try (AccessibilityBackend backend = new AccessibilityBackend()) {
      socket.setSoTimeout(5000);
      InputStream in = new BufferedInputStream(socket.getInputStream(), 65536);
      OutputStream out = new BufferedOutputStream(socket.getOutputStream(), 65536);
      byte[] random = new byte[32];
      new SecureRandom().nextBytes(random);
      String nonce =
          Base64.encodeToString(random, Base64.URL_SAFE | Base64.NO_WRAP | Base64.NO_PADDING);
      write(out, new JSONObject().put("protocol", 1).put("nonce", nonce));
      String first = line(in, 2048);
      if (first == null) return;
      JSONObject auth = new JSONObject(first);
      boolean ok =
          auth.optInt("protocol") == 1
              && MessageDigest.isEqual(
                  proof(state.token(), "client:" + nonce).getBytes(StandardCharsets.UTF_8),
                  auth.optString("proof").getBytes(StandardCharsets.UTF_8));
      write(
          out,
          new JSONObject()
              .put("ok", ok)
              .put("proof", ok ? proof(state.token(), "server:" + nonce) : ""));
      if (!ok) return;
      authenticated = true;
      clientsChanged.accept(1);
      socket.setSoTimeout(5 * 60 * 1000);
      for (String text;
          !closed && state.enabled() && (text = line(in, 16 * 1024 * 1024)) != null; ) {
        clientsChanged.accept(0);
        JSONObject response = new JSONObject();
        try {
          JSONObject request = new JSONObject(text);
          response.put("id", request.optString("id"));
          JSONObject params = request.optJSONObject("params");
          if (params == null) params = new JSONObject();
          if (request.optString("id").length() > 80 || params.optString("text").length() > 16384)
            throw new IllegalArgumentException("Request exceeds limits");
          response
              .put("result", backend.dispatch(request.optString("method"), params))
              .put("ok", true);
        } catch (Exception e) {
          Throwable cause = e;
          while (cause.getCause() != null) cause = cause.getCause();
          if(cause.getMessage()!=null&&cause.getMessage().startsWith("background_not_supported:"))response.put("error_code","background_not_supported");
          response
              .put("ok", false)
              .put(
                  "error",
                  cause instanceof IllegalArgumentException
                          || cause instanceof IllegalStateException
                      ? cause.getMessage()
                      : "Android auxiliary operation failed; observe again");
        }
        write(out, response);
      }
    } catch (Exception ignored) {
    } finally {
      sockets.remove(socket);
      try {
        socket.close();
      } catch (Exception ignored) {
      }
      if (authenticated) clientsChanged.accept(-1);
    }
  }

  private static String proof(String token, String message) throws Exception {
    Mac mac = Mac.getInstance("HmacSHA256");
    mac.init(new SecretKeySpec(token.getBytes(StandardCharsets.UTF_8), "HmacSHA256"));
    return Base64.encodeToString(
        mac.doFinal(message.getBytes(StandardCharsets.UTF_8)),
        Base64.URL_SAFE | Base64.NO_WRAP | Base64.NO_PADDING);
  }

  public void close() {
    closed = true;
    try {
      listener.close();
    } catch (Exception ignored) {
    }
    for (Socket s : sockets)
      try {
        s.close();
      } catch (Exception ignored) {
      }
    workers.shutdownNow();
  }
}
