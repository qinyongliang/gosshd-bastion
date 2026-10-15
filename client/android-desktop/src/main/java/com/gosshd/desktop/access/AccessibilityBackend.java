package com.gosshd.desktop.access;

import android.accessibilityservice.AccessibilityService;
import android.accessibilityservice.GestureDescription;
import android.app.KeyguardManager;
import android.content.Intent;
import android.graphics.Path;
import android.graphics.Rect;
import android.os.Build;
import android.os.Handler;
import android.os.Looper;
import android.os.PowerManager;
import android.os.SystemClock;
import android.util.DisplayMetrics;
import android.view.WindowManager;
import android.view.accessibility.AccessibilityNodeInfo;
import android.view.accessibility.AccessibilityWindowInfo;
import java.util.UUID;
import java.util.concurrent.Callable;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.TimeUnit;
import org.json.JSONArray;
import org.json.JSONObject;

public final class AccessibilityBackend implements AutoCloseable {
  private final Observation observation = new Observation();
  private final Handler main = new Handler(Looper.getMainLooper());

  private DesktopAccessibilityService service() {
    DesktopAccessibilityService s = DesktopAccessibilityService.connected();
    if (s == null) throw new IllegalStateException("Enable accessibility locally first");
    return s;
  }

  private <T> T ui(Callable<T> task) throws Exception {
    CompletableFuture<T> future = new CompletableFuture<>();
    main.post(
        () -> {
          if (future.isCancelled()) return;
          try {
            future.complete(task.call());
          } catch (Exception e) {
            future.completeExceptionally(e);
          }
        });
    try {
      return future.get(10, TimeUnit.SECONDS);
    } catch (Exception e) {
      future.cancel(false);
      throw e;
    }
  }

  private void interactive() {
    DesktopAccessibilityService s = service();
    if (!((PowerManager) s.getSystemService(PowerManager.class)).isInteractive())
      throw new IllegalStateException("Screen is off");
    if (((KeyguardManager) s.getSystemService(KeyguardManager.class)).isKeyguardLocked())
      throw new IllegalStateException("Device is locked");
  }

  private Rect display() {
    DisplayMetrics m = new DisplayMetrics();
    ((WindowManager) service().getSystemService(WindowManager.class))
        .getDefaultDisplay()
        .getRealMetrics(m);
    return new Rect(0, 0, m.widthPixels, m.heightPixels);
  }

  private AccessibilityWindowInfo window(JSONObject p) throws Exception {
    JSONObject w = p.getJSONObject("window");
    int id = Integer.parseInt(w.getString("id"));
    String app = w.getString("app");
    for (AccessibilityWindowInfo item : service().getWindows())
      if (item.getId() == id) {
        AccessibilityNodeInfo root = item.getRoot();
        if (root != null) {
          String actual = String.valueOf(root.getPackageName());
          root.recycle();
          if (actual.equals(app)) return item;
        }
      }
    throw new IllegalStateException("Window disappeared; enumerate again");
  }

  private JSONObject descriptor(AccessibilityWindowInfo w) throws Exception {
    AccessibilityNodeInfo root = w.getRoot();
    if (root == null) throw new IllegalStateException("Window has no accessible root");
    String app = String.valueOf(root.getPackageName());
    root.recycle();
    return new JSONObject()
        .put("id", Integer.toString(w.getId()))
        .put("app", app)
        .put("title", AccessibilityTree.text(w.getTitle(), 512));
  }

  private JSONArray windows() throws Exception {
    JSONArray out = new JSONArray();
    for (AccessibilityWindowInfo w : service().getWindows()) {
      if (w.getType() != AccessibilityWindowInfo.TYPE_APPLICATION
          && w.getType() != AccessibilityWindowInfo.TYPE_SYSTEM
          && w.getType() != AccessibilityWindowInfo.TYPE_INPUT_METHOD) continue;
      try {
        out.put(descriptor(w));
      } catch (Exception ignored) {
      }
      if (out.length() >= 512) break;
    }
    return out;
  }

  private void verify(JSONObject p) throws Exception {
    if (observation.id == null
        || !observation.id.equals(p.optString("snapshot_id"))
        || SystemClock.elapsedRealtime() - observation.at > 60000)
      throw new IllegalStateException("Stale observation; observe again");
    AccessibilityWindowInfo w = window(p);
    Rect b = new Rect();
    w.getBoundsInScreen(b);
    b.intersect(display());
    if (w.getId() != observation.window
        || !observation.app.equals(p.getJSONObject("window").getString("app"))
        || !b.equals(observation.bounds)
        || !display().equals(observation.display))
      throw new IllegalStateException("Window geometry changed");
    if (!w.isActive() && !w.isFocused())
      throw new IllegalStateException("Target is not active; activate locally and observe again");
    if (service().getPackageName().equals(observation.app))
      throw new IllegalStateException(
          "Companion permission and pairing screens require local operation");
  }

  private AccessibilityNodeInfo node(JSONObject p) throws Exception {
    int index = p.getInt("element_index");
    if (index < 0 || index >= observation.nodes.size())
      throw new IllegalStateException("Unknown element index");
    AccessibilityNodeInfo n = observation.nodes.get(index);
    if (!n.refresh()
        || !n.isEnabled()
        || n.isPassword()
        || !n.isVisibleToUser()
        || !observation.app.equals(String.valueOf(n.getPackageName())))
      throw new IllegalStateException("Element no longer available");
    return n;
  }

  public JSONObject dispatch(String method, JSONObject p) throws Exception {
    try {
      return execute(method, p);
    } catch (Exception error) {
      ui(
          () -> {
            observation.clear();
            return null;
          });
      throw error;
    }
  }

  private JSONObject execute(String method, JSONObject p) throws Exception {
    if (method.equals("status")) {
      DesktopAccessibilityService s = DesktopAccessibilityService.connected();
      boolean awake =
          s != null && ((PowerManager) s.getSystemService(PowerManager.class)).isInteractive();
      boolean locked =
          s == null
              || ((KeyguardManager) s.getSystemService(KeyguardManager.class)).isKeyguardLocked();
      return new JSONObject()
          .put("platform", "android")
          .put("backend", "accessibility").put("background_supported",false)
          .put("uid", android.os.Process.myUid())
          .put("root", false)
          .put("interactive", s != null && awake && !locked)
          .put("screen_on", awake)
          .put("locked", locked)
          .put("protocol_version", 1)
          .put("semantic_actions", true)
          .put("unicode_text", true)
          .put("set_value", true)
          .put(
              "permissions",
              new JSONObject().put("accessibility", s != null).put("screen_capture", s != null))
          .put("capture_backend", "AccessibilityService.takeScreenshot")
          .put("primary_display_only", true);
    }
    if (method.equals("request_permissions")) {
      ui(
          () -> {
            observation.clear();
            return null;
          });
      return new JSONObject()
          .put("ok", false)
          .put(
              "message",
              "Use the companion app locally for accessibility, ADB or Shizuku activation; remote"
                  + " RPC never grants permissions");
    }
    interactive();
    if (method.equals("list_windows")) return ui(() -> new JSONObject().put("windows", windows()));
    if (method.equals("list_apps"))
      return ui(
          () -> {
            JSONArray list = windows(), apps = new JSONArray();
            java.util.LinkedHashMap<String, JSONArray> groups = new java.util.LinkedHashMap<>();
            for (int i = 0; i < list.length(); i++) {
              JSONObject w = list.getJSONObject(i);
              String app = w.getString("app");
              if (!groups.containsKey(app)) groups.put(app, new JSONArray());
              groups.get(app).put(w);
            }
            for (String app : groups.keySet())
              apps.put(new JSONObject().put("id", app).put("windows", groups.get(app)));
            return new JSONObject().put("apps", apps);
          });
    if (method.equals("list_displays"))
      return ui(
          () ->
              new JSONObject()
                  .put(
                      "displays",
                      new JSONArray()
                          .put(
                              new JSONObject()
                                  .put("id", "0")
                                  .put("primary", true)
                                  .put("bounds", AccessibilityTree.rect(display())))));
    if (method.equals("get_window_state") || method.equals("screenshot")) {
      ui(
          () -> {
            observation.clear();
            return null;
          });
      boolean observe = method.equals("get_window_state");
      JSONObject result =
          ui(
              () -> {
                Rect bounds;
                JSONObject w = new JSONObject().put("id", "").put("app", "");
                AccessibilityWindowInfo info = null;
                if (p.has("window")) {
                  info = window(p);
                  w = descriptor(info);
                  bounds = new Rect();
                  info.getBoundsInScreen(bounds);
                  if (!bounds.intersect(display()))
                    throw new IllegalStateException("Window is outside display");
                } else bounds = display();
                if (!p.optString("display_id", "0").equals("0")
                    && !p.optString("display_id").isEmpty())
                  throw new IllegalStateException("Only display 0 is supported");
                if (bounds.width() < 1 || bounds.height() < 1)
                  throw new IllegalStateException("Window has no visible bounds");
                observation.bounds = bounds;
                observation.display = display();
                observation.app = w.getString("app");
                observation.window = info == null ? -1 : info.getId();
                JSONObject value =
                    new JSONObject()
                        .put("snapshot_id", UUID.randomUUID().toString())
                        .put("window", w)
                        .put("bounds", AccessibilityTree.rect(bounds));
                if (observe && p.optBoolean("include_text", false)) {
                  int depth = p.optInt("max_depth", 8), limit = p.optInt("max_elements", 512);
                  if (depth == 0) depth = 8;
                  if (limit == 0) limit = 512;
                  if (depth < 1 || depth > 16 || limit < 1 || limit > 1024)
                    throw new IllegalArgumentException("Accessibility limits exceeded");
                  if (Build.VERSION.SDK_INT >= 33) service().clearCache();
                  AccessibilityTree tree = new AccessibilityTree(observation, depth, limit);
                  value
                      .put("elements", tree.build(info.getRoot()))
                      .put("truncated", tree.truncated());
                }
                return value;
              });
      if (!observe || p.optBoolean("include_screenshot", true))
        result.put("image", ScreenCapture.capture(service(), observation.bounds));
      ui(
          () -> {
            if (observe) {
              AccessibilityWindowInfo now = window(p);
              Rect b = new Rect();
              now.getBoundsInScreen(b);
              b.intersect(display());
              if (!b.equals(observation.bounds) || !display().equals(observation.display))
                throw new IllegalStateException("Window changed during capture");
              observation.id = result.getString("snapshot_id");
              observation.at = SystemClock.elapsedRealtime();
            } else observation.clear();
            return null;
          });
      return result;
    }
    if (method.equals("launch_app")) {
      ui(
          () -> {
            observation.clear();
            String app = p.getString("app");
            if (!app.matches("[A-Za-z_][A-Za-z0-9_]*(\\.[A-Za-z_][A-Za-z0-9_]*)+"))
              throw new IllegalArgumentException("Package identifier required");
            Intent intent = service().getPackageManager().getLaunchIntentForPackage(app);
            if (intent == null) throw new IllegalStateException("No launcher activity");
            intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK);
            service().startActivity(intent);
            return null;
          });
      return new JSONObject().put("ok", true);
    }
    try {
      ui(
          () -> {
            verify(p);
            observation.buttonIsLeft = p.optString("button", "left").equals("left");
            return null;
          });
      if (method.equals("click") && !p.has("element_index")
          || method.equals("drag")
          || method.equals("scroll") && !p.has("element_index")) {
        double x = p.optDouble("x", 0),
            y = p.optDouble("y", 0),
            tx = p.optDouble("to_x", x),
            ty = p.optDouble("to_y", y);
        int duration = method.equals("drag") ? 300 : method.equals("scroll") ? 240 : 60;
        if (method.equals("scroll")) {
          tx = Math.max(0, Math.min(observation.bounds.width() - 1, x - p.optDouble("delta_x", 0)));
          ty =
              Math.max(0, Math.min(observation.bounds.height() - 1, y - p.optDouble("delta_y", 0)));
        }
        if (!method.equals("drag") && !method.equals("scroll")) {
          tx = x;
          ty = y;
        }
        gesture(x, y, tx, ty, duration);
        if (method.equals("click") && p.optInt("click_count", 1) == 2) {
          Thread.sleep(80);
          gesture(x, y, tx, ty, duration);
        }
      } else
        ui(
            () -> {
              NodeActions.execute(
                  method, p, service(), window(p), p.has("element_index") ? node(p) : null);
              return null;
            });
      return new JSONObject().put("ok", true);
    } finally {
      ui(
          () -> {
            observation.clear();
            return null;
          });
    }
  }

  private void gesture(double x, double y, double tx, double ty, int duration) throws Exception {
    if (!observation.buttonIsLeft)
      throw new IllegalArgumentException("Only left gestures are supported");
    Rect b = observation.bounds;
    for (double v : new double[] {x, y, tx, ty})
      if (!Double.isFinite(v)) throw new IllegalArgumentException("Invalid coordinate");
    if (x < 0
        || y < 0
        || tx < 0
        || ty < 0
        || x >= b.width()
        || tx >= b.width()
        || y >= b.height()
        || ty >= b.height()) throw new IllegalArgumentException("Gesture outside observed bounds");
    CompletableFuture<Boolean> done = new CompletableFuture<>();
    ui(
        () -> {
          interactive();
          Rect gestureRegion =
              new Rect(
                  b.left + (int) Math.min(x, tx),
                  b.top + (int) Math.min(y, ty),
                  b.left + (int) Math.max(x, tx) + 1,
                  b.top + (int) Math.max(y, ty) + 1);
          for (AccessibilityWindowInfo above : service().getWindows()) {
            if (above.getId() == observation.window) break;
            Rect cover = new Rect();
            above.getBoundsInScreen(cover);
            if (Rect.intersects(cover, gestureRegion))
              throw new IllegalStateException("Gesture is occluded by another visible window");
          }
          Path path = new Path();
          path.moveTo(b.left + (float) x, b.top + (float) y);
          path.lineTo(b.left + (float) tx, b.top + (float) ty);
          GestureDescription g =
              new GestureDescription.Builder()
                  .addStroke(new GestureDescription.StrokeDescription(path, 0, duration))
                  .build();
          if (!service()
              .dispatchGesture(
                  g,
                  new AccessibilityService.GestureResultCallback() {
                    public void onCompleted(GestureDescription ignored) {
                      done.complete(true);
                    }

                    public void onCancelled(GestureDescription ignored) {
                      done.complete(false);
                    }
                  },
                  main)) done.complete(false);
          return null;
        });
    if (!done.get(5, TimeUnit.SECONDS)) throw new IllegalStateException("Gesture canceled");
  }

  public void close() {
    main.post(observation::clear);
  }
}
