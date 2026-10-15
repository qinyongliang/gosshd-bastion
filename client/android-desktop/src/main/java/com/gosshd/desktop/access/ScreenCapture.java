package com.gosshd.desktop.access;

import android.accessibilityservice.AccessibilityService;
import android.graphics.Bitmap;
import android.graphics.Rect;
import android.util.Base64;
import java.io.ByteArrayOutputStream;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.TimeUnit;
import org.json.JSONObject;

public final class ScreenCapture {
  // Called by the worker, never block the service/main callback executor.
  private static long capturedAt;

  public static synchronized JSONObject capture(DesktopAccessibilityService service, Rect crop)
      throws Exception {
    long delay = 400 - (android.os.SystemClock.elapsedRealtime() - capturedAt);
    if (delay > 0) Thread.sleep(delay);
    capturedAt = android.os.SystemClock.elapsedRealtime();
    CompletableFuture<JSONObject> result = new CompletableFuture<>();
    service.takeScreenshot(
        0,
        service.getMainExecutor(),
        new AccessibilityService.TakeScreenshotCallback() {
          public void onFailure(int code) {
            result.completeExceptionally(
                new IllegalStateException("Screenshot denied by Android (code " + code + ")"));
          }

          public void onSuccess(AccessibilityService.ScreenshotResult shot) {
            Bitmap hardware = null, software = null, region = null, scaled = null;
            try {
              hardware = Bitmap.wrapHardwareBuffer(shot.getHardwareBuffer(), shot.getColorSpace());
              if (hardware == null) throw new IllegalStateException("Empty screenshot");
              software = hardware.copy(Bitmap.Config.ARGB_8888, false);
              Rect frame =
                  crop == null ? new Rect(0, 0, software.getWidth(), software.getHeight()) : crop;
              if (frame.left < 0
                  || frame.top < 0
                  || frame.right > software.getWidth()
                  || frame.bottom > software.getHeight())
                throw new IllegalStateException(
                    "Screenshot geometry changed: region "
                        + frame.toShortString()
                        + " image "
                        + software.getWidth()
                        + "x"
                        + software.getHeight());
              region =
                  Bitmap.createBitmap(
                      software, frame.left, frame.top, frame.width(), frame.height());
              double factor = Math.min(1.0, 1600.0 / Math.max(frame.width(), frame.height()));
              scaled =
                  Bitmap.createScaledBitmap(
                      region,
                      Math.max(1, (int) (frame.width() * factor)),
                      Math.max(1, (int) (frame.height() * factor)),
                      true);
              ByteArrayOutputStream bytes = new ByteArrayOutputStream();
              scaled.compress(Bitmap.CompressFormat.PNG, 100, bytes);
              if (bytes.size() > 8 * 1024 * 1024)
                throw new IllegalStateException("Screenshot exceeds image limit");
              result.complete(
                  new JSONObject()
                      .put("mime_type", "image/png")
                      .put("data", Base64.encodeToString(bytes.toByteArray(), Base64.NO_WRAP))
                      .put("width", scaled.getWidth())
                      .put("height", scaled.getHeight()));
            } catch (Exception e) {
              result.completeExceptionally(e);
            } finally {
              shot.getHardwareBuffer().close();
              if (scaled != null) scaled.recycle();
              if (region != null && region != scaled) region.recycle();
              if (software != null && software != region) software.recycle();
              if (hardware != null) hardware.recycle();
            }
          }
        });
    return result.get(10, TimeUnit.SECONDS);
  }
}
