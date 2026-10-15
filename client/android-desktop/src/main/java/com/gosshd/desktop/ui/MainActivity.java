package com.gosshd.desktop.ui;

import android.Manifest;
import android.app.Activity;
import android.content.*;
import android.content.pm.PackageManager;
import android.os.*;
import android.provider.Settings;
import android.widget.*;
import com.gosshd.desktop.access.DesktopAccessibilityService;
import com.gosshd.desktop.activation.*;
import com.gosshd.desktop.lifecycle.ControlService;
import com.gosshd.desktop.local.LocalState;

public final class MainActivity extends Activity {
  private LocalState state;
  private TextView status;
  private String lastMessage = "";
  private ShizukuActivation shizuku;
  private final Handler timer = new Handler(Looper.getMainLooper());
  private final Runnable refresh =
      new Runnable() {
        public void run() {
          status.setText(
              "Auxiliary mode (no root)\nAccessibility: "
                  + (DesktopAccessibilityService.connected() != null ? "connected" : "disabled")
                  + "\nLocal control: "
                  + (state.enabled() ? "enabled" : "stopped")
                  + "\n"
                  + "Root/ADB shell mode remains available in Agent.\n"
                  + "Pair only with your local Agent.\n"
                  + "Shizuku ADB mode needs restarting after reboot.\n"
                  + lastMessage);
          timer.postDelayed(this, 1500);
        }
      };

  private void button(LinearLayout view, String label, Runnable action) {
    Button b = new Button(this);
    b.setText(label);
    view.addView(b);
    b.setOnClickListener(v -> action.run());
  }

  private void feedback(String value) {
    lastMessage = value;
    Toast.makeText(this, value, Toast.LENGTH_LONG).show();
  }

  private void copy(String label, String value) {
    ((ClipboardManager) getSystemService(CLIPBOARD_SERVICE))
        .setPrimaryClip(ClipData.newPlainText(label, value));
    feedback(label + " copied locally");
  }

  @Override
  public void onCreate(Bundle saved) {
    super.onCreate(saved);
    state = new LocalState(this);
    shizuku = new ShizukuActivation(this, this::feedback);
    ScrollView scroll = new ScrollView(this);
    LinearLayout view = new LinearLayout(this);
    view.setOrientation(LinearLayout.VERTICAL);
    view.setPadding(32, 32, 32, 32);
    scroll.addView(view);
    status = new TextView(this);
    status.setTextSize(17);
    view.addView(status);
    button(
        view,
        "Enable accessibility manually",
        () -> startActivity(new Intent(Settings.ACTION_ACCESSIBILITY_SETTINGS)));
    button(
        view,
        "Copy ADB activation grant",
        () ->
            copy(
                "ADB command",
                "adb shell pm grant com.gosshd.desktop android.permission.WRITE_SECURE_SETTINGS"));
    button(
        view,
        "Activate with ADB grant",
        () -> {
          if (checkSelfPermission(Manifest.permission.WRITE_SECURE_SETTINGS)
              != PackageManager.PERMISSION_GRANTED) {
            feedback("Run the copied ADB grant first");
            return;
          }
          try {
            AccessibilityActivation.activate(this);
            feedback("Activation requested; other accessibility services preserved");
          } catch (Exception e) {
            feedback("Activation rejected by Android");
          }
        });
    button(view, "Activate with Shizuku", shizuku::request);
    Switch control = new Switch(this);
    control.setText("Enable paired local control / foreground service");
    control.setChecked(state.enabled());
    view.addView(control);
    control.setOnCheckedChangeListener(
        (b, on) -> {
          state.enabled(on);
          if (on) {
            if (Build.VERSION.SDK_INT >= 33
                && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS)
                    != PackageManager.PERMISSION_GRANTED)
              requestPermissions(new String[] {Manifest.permission.POST_NOTIFICATIONS}, 7);
            ControlService.start(this);
          } else stopService(new Intent(this, ControlService.class));
        });
    Switch boot = new Switch(this);
    boot.setText("Restore control after boot (best effort)");
    boot.setChecked(state.boot());
    view.addView(boot);
    boot.setOnCheckedChangeListener((b, on) -> state.boot(on));
    button(
        view,
        "Battery optimization settings",
        () -> startActivity(new Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS)));
    button(view, "Copy pairing token", () -> copy("Pairing token", state.token()));
    button(
        view,
        "Rotate token / disconnect clients",
        () -> {
          stopService(new Intent(this, ControlService.class));
          state.rotate();
          if (state.enabled()) ControlService.start(this);
          feedback("Old token revoked; pair your Agent again");
        });
    TextView guide = new TextView(this);
    guide.setText(
        "In Termux: save the pairing token to a private chmod-600 file, then run Agent with"
            + " --desktop-enable --android-backend accessibility --android-bridge-token-file"
            + " <file>. No root/ADB/Shizuku is needed for ongoing auxiliary actions after"
            + " accessibility is enabled.\n"
            + "Foreground notification, boot receiver and bounded session wake lock improve"
            + " reliability; Android force-stop and vendor battery restrictions cannot be bypassed."
            + " Add the app to your vendor auto-start allowlist if needed.\n"
            + "Shell mode: --android-backend shell, requires root/adb shell. APK mode: Android 11+"
            + " screenshots, semantic node operations and Unicode text.");
    view.addView(guide);
    setContentView(scroll);
  }

  @Override
  public void onResume() {
    super.onResume();
    if (state.enabled()) ControlService.start(this);
    timer.post(refresh);
  }

  @Override
  public void onPause() {
    timer.removeCallbacks(refresh);
    super.onPause();
  }

  @Override
  public void onDestroy() {
    shizuku.close();
    super.onDestroy();
  }
}
