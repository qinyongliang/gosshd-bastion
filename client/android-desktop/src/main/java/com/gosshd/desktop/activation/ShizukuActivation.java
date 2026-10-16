package com.gosshd.desktop.activation;

import android.app.Activity;
import android.content.ComponentName;
import android.content.ServiceConnection;
import android.content.pm.PackageManager;
import android.os.IBinder;
import com.gosshd.desktop.R;
import java.util.function.Consumer;
import rikka.shizuku.Shizuku;

public final class ShizukuActivation implements AutoCloseable {
  private final Activity activity;
  private final Consumer<String> feedback;
  private final Shizuku.UserServiceArgs args;
  private boolean bound;
  private final Shizuku.OnRequestPermissionResultListener permission;
  private final ServiceConnection connection =
      new ServiceConnection() {
        public void onServiceConnected(ComponentName name, IBinder binder) {
          new Thread(
                  () -> {
                    try {
                      String result = IActivation.Stub.asInterface(binder).activate();
                      int message =
                          result.startsWith("Activation requested")
                              ? R.string.activation_requested_async
                              : R.string.shizuku_activation_failed;
                      activity.runOnUiThread(() -> feedback.accept(activity.getString(message)));
                    } catch (Exception e) {
                      activity.runOnUiThread(
                          () -> feedback.accept(activity.getString(R.string.shizuku_activation_failed)));
                    } finally {
                      try {
                        Shizuku.unbindUserService(args, connection, true);
                        bound = false;
                      } catch (Exception ignored) {
                      }
                    }
                  },
                  "shizuku-activation")
              .start();
        }

        public void onServiceDisconnected(ComponentName name) {
          bound = false;
        }
      };

  public ShizukuActivation(Activity activity, Consumer<String> feedback) {
    this.activity = activity;
    this.feedback = feedback;
    permission =
        (code, result) -> {
          if (code == 42) {
            if (result == PackageManager.PERMISSION_GRANTED) bind();
            else feedback.accept(activity.getString(R.string.shizuku_permission_denied));
          }
        };
    args =
        new Shizuku.UserServiceArgs(new ComponentName(activity, ActivationUserService.class))
            .daemon(false)
            .processNameSuffix("activation")
            .version(1);
    Shizuku.addRequestPermissionResultListener(permission);
  }

  public void request() {
    try {
      if (!Shizuku.pingBinder()) {
        feedback.accept(activity.getString(R.string.shizuku_start_first));
        return;
      }
      if (Shizuku.checkSelfPermission() == PackageManager.PERMISSION_GRANTED) bind();
      else Shizuku.requestPermission(42);
    } catch (Exception e) {
      feedback.accept(activity.getString(R.string.shizuku_unavailable));
    }
  }

  private void bind() {
    try {
      Shizuku.bindUserService(args, connection);
      bound = true;
      feedback.accept(activity.getString(R.string.shizuku_activating));
    } catch (Exception e) {
      feedback.accept(activity.getString(R.string.shizuku_bind_failed));
    }
  }

  public void close() {
    Shizuku.removeRequestPermissionResultListener(permission);
    if (bound) {
      try {
        Shizuku.unbindUserService(args, connection, true);
      } catch (Exception ignored) {
      }
    }
  }
}
