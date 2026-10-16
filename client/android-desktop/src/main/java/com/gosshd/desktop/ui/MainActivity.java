package com.gosshd.desktop.ui;

import android.Manifest;
import android.app.Activity;
import android.content.*;
import android.content.pm.PackageManager;
import android.os.*;
import android.provider.Settings;
import android.text.InputType;
import android.view.View;
import android.widget.*;
import com.gosshd.desktop.R;
import com.gosshd.desktop.access.DesktopAccessibilityService;
import com.gosshd.desktop.activation.*;
import com.gosshd.desktop.lifecycle.ControlService;
import com.gosshd.desktop.local.LocalState;
import com.gosshd.desktop.local.AgentSettings;

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
              getString(
                  R.string.status,
                  getString(
                      DesktopAccessibilityService.connected() != null
                          ? R.string.status_connected
                          : R.string.status_disabled),
                  getString(state.enabled() ? R.string.status_enabled : R.string.status_stopped),
                  lastMessage,
                  getString(ControlService.agentStatus())));
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
    feedback(getString(R.string.copied_locally, label));
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
    agentSetup(view);
    button(
        view,
        getString(R.string.enable_accessibility_manually),
        () -> startActivity(new Intent(Settings.ACTION_ACCESSIBILITY_SETTINGS)));
    button(
        view,
        getString(R.string.copy_adb_activation_grant),
        () ->
            copy(
                getString(R.string.adb_command),
                "adb shell pm grant com.gosshd.desktop android.permission.WRITE_SECURE_SETTINGS"));
    button(
        view,
        getString(R.string.activate_with_adb),
        () -> {
          if (checkSelfPermission(Manifest.permission.WRITE_SECURE_SETTINGS)
              != PackageManager.PERMISSION_GRANTED) {
            feedback(getString(R.string.run_adb_grant_first));
            return;
          }
          try {
            AccessibilityActivation.activate(this);
            feedback(getString(R.string.activation_requested_preserved));
          } catch (Exception e) {
            feedback(getString(R.string.activation_rejected));
          }
        });
    button(view, getString(R.string.activate_with_shizuku), shizuku::request);
    Switch control = new Switch(this);
    control.setText(R.string.enable_local_control);
    control.setChecked(state.enabled());
    view.addView(control);
    control.setOnCheckedChangeListener(
        (b, on) -> {
          state.enabled(on);
          if (on) requestNotifications();
          ControlService.start(this);
        });
    Switch boot = new Switch(this);
    boot.setText(R.string.restore_control_after_boot);
    boot.setChecked(state.boot());
    view.addView(boot);
    boot.setOnCheckedChangeListener((b, on) -> state.boot(on));
    button(
        view,
        getString(R.string.battery_optimization_settings),
        () -> startActivity(new Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS)));
    button(
        view,
        getString(R.string.copy_pairing_token),
        () -> copy(getString(R.string.pairing_token), state.token()));
    button(
        view,
        getString(R.string.rotate_token_disconnect_clients),
        () -> {
          state.rotate();
          ControlService.refresh(this);
          feedback(getString(R.string.old_token_revoked));
        });
    TextView guide = new TextView(this);
    guide.setText(
        getString(R.string.setup_guide));
    view.addView(guide);
    setContentView(scroll);
  }


  private void requestNotifications() {
    if (Build.VERSION.SDK_INT >= 33 && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS)
        != PackageManager.PERMISSION_GRANTED)
      requestPermissions(new String[] {Manifest.permission.POST_NOTIFICATIONS}, 7);
  }

  private void agentSetup(LinearLayout view) {
    AgentSettings settings = new AgentSettings(this);
    TextView serverLabel = new TextView(this);
    serverLabel.setText(R.string.server_address);
    view.addView(serverLabel);
    EditText server = new EditText(this);
    server.setHint("https://bastion.example.com");
    server.setSingleLine(true);
    server.setContentDescription(getString(R.string.server_address));
    server.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_URI);
    server.setText(settings.server());
    view.addView(server);
    TextView tokenLabel = new TextView(this);
    tokenLabel.setText(R.string.enrollment_token);
    view.addView(tokenLabel);
    EditText token = new EditText(this);
    token.setSingleLine(true);
    token.setContentDescription(getString(R.string.enrollment_token));
    token.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_PASSWORD);
    token.setSaveEnabled(false);
    token.setImportantForAutofill(View.IMPORTANT_FOR_AUTOFILL_NO_EXCLUDE_DESCENDANTS);
    token.setText(settings.token());
    view.addView(token);
    TextView help = new TextView(this);
    help.setText(R.string.connection_help);
    view.addView(help);
    button(view, getString(R.string.save_start_agent), () -> {
      try {
        settings.save(server.getText().toString(), token.getText().toString());
        server.setText(settings.server());
        state.agentEnabled(true);
        requestNotifications();
        ControlService.start(this);
        feedback(getString(R.string.connection_saved));
      } catch (IllegalArgumentException error) {
        feedback(getString(R.string.connection_invalid));
      } catch (Exception error) {
        feedback(getString(R.string.agent_configuration_error));
      }
    });
    button(view, getString(R.string.stop_agent), () -> {
      state.agentEnabled(false);
      ControlService.start(this);
      feedback(getString(R.string.agent_stopped));
    });
  }

  @Override
  public void onResume() {
    super.onResume();
    if (state.shouldRun()) ControlService.start(this);
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
