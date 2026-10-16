package com.gosshd.desktop.lifecycle;

import android.app.*;
import android.content.*;
import android.os.*;
import com.gosshd.desktop.R;
import com.gosshd.desktop.local.AgentSettings;
import com.gosshd.desktop.local.LocalState;
import com.gosshd.desktop.rpc.BridgeServer;
import com.gosshd.desktop.ui.MainActivity;

// User opt-in owns both the packaged Agent and optional local control bridge.
public final class ControlService extends Service {
  public static final String STOP = "com.gosshd.desktop.STOP";
  private static final String REFRESH = "com.gosshd.desktop.REFRESH";
  private static volatile int agentStatus = R.string.agent_stopped;
  private final Handler main = new Handler(Looper.getMainLooper());
  private BridgeServer bridge;
  private int bridgeGeneration;
  private AgentRuntime agent;
  private String configurationKey = "";
  private int generation;
  private PowerManager.WakeLock lock;
  private int clients;

  public static int agentStatus() { return agentStatus; }
  public static void start(Context context) { start(context, ""); }
  public static void refresh(Context context) { start(context, REFRESH); }
  private static void start(Context context, String action) {
    if (!new LocalState(context).shouldRun()) {
      context.stopService(new Intent(context, ControlService.class));
      return;
    }
    try { context.startForegroundService(new Intent(context, ControlService.class).setAction(action)); }
    catch (RuntimeException ignored) { /* Owner can reopen the visible activity. */ }
  }

  @Override public void onCreate() {
    super.onCreate();
    getSystemService(NotificationManager.class).createNotificationChannel(
        new NotificationChannel("desktop-control", getString(R.string.notification_channel), NotificationManager.IMPORTANCE_LOW));
    lock = getSystemService(PowerManager.class).newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "gosshd:desktop-session");
    lock.setReferenceCounted(false);
  }

  @Override public int onStartCommand(Intent intent, int flags, int startId) {
    LocalState state = new LocalState(this);
    if (intent != null && STOP.equals(intent.getAction())) {
      state.stopAll();
      stopSelf();
      return START_NOT_STICKY;
    }
    if (!state.shouldRun()) { stopSelf(); return START_NOT_STICKY; }
    startForeground(41, notification());
    if (intent != null && REFRESH.equals(intent.getAction())) closeBridge();
    if (!state.enabled()) closeBridge();
    else if (bridge == null) {
      try {
        final int currentBridge = ++bridgeGeneration;
        bridge = new BridgeServer(state, delta -> main.post(() -> {
          if (currentBridge != bridgeGeneration) return;
          clients = Math.max(0, clients + delta);
          if (clients > 0) lock.acquire(10 * 60 * 1000L);
          else if (lock.isHeld()) lock.release();
          updateNotification();
        }));
      } catch (Exception error) { state.enabled(false); }
    }
    if (!state.shouldRun()) { stopSelf(); return START_NOT_STICKY; }
    synchronizeAgent(state);
    updateNotification();
    return START_STICKY;
  }

  private void synchronizeAgent(LocalState state) {
    AgentSettings settings = new AgentSettings(this);
    if (!state.agentEnabled() || !settings.configured()) { stopAgent(); return; }
    try {
      String json = settings.configuration(state);
      String key = json + (state.enabled() ? state.token() : "");
      if (agent != null && key.equals(configurationKey)) return;
      stopAgent();
      final int current = ++generation;
      agent = new AgentRuntime(this, settings.writeConfiguration(json, state), status -> main.post(() -> {
        if (current != generation) return;
        agentStatus = status;
        updateNotification();
      }));
      configurationKey = key;
      agent.start();
    } catch (Exception ignored) { agentStatus = R.string.agent_configuration_error; }
  }

  private void stopAgent() {
    generation++;
    if (agent != null) { agent.close(); agent = null; }
    configurationKey = "";
    agentStatus = R.string.agent_stopped;
  }
  private void closeBridge() {
    bridgeGeneration++;
    if (bridge != null) { bridge.close(); bridge = null; }
    clients = 0;
    if (lock != null && lock.isHeld()) lock.release();
  }
  private void updateNotification() {
    getSystemService(NotificationManager.class).notify(41, notification());
  }
  private Notification notification() {
    PendingIntent open = PendingIntent.getActivity(this, 0, new Intent(this, MainActivity.class), PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);
    PendingIntent stop = PendingIntent.getService(this, 1, new Intent(this, ControlService.class).setAction(STOP), PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);
    return new Notification.Builder(this, "desktop-control")
        .setSmallIcon(android.R.drawable.ic_menu_view)
        .setContentTitle(getString(R.string.notification_title))
        .setContentText(getString(R.string.notification_agent_status, getString(agentStatus)))
        .setStyle(new Notification.BigTextStyle().bigText(getString(R.string.notification_agent_status, getString(agentStatus)) + "\n" + getResources().getQuantityString(R.plurals.notification_sessions, clients, clients)))
        .setContentIntent(open).setOngoing(true)
        .addAction(new Notification.Action.Builder(null, getString(R.string.stop_control), stop).build()).build();
  }
  @Override public void onDestroy() {
    stopAgent();
    closeBridge();
    main.removeCallbacksAndMessages(null);
    super.onDestroy();
  }
  @Override public IBinder onBind(Intent intent) { return null; }
}
