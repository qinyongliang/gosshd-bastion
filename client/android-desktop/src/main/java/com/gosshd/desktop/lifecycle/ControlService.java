package com.gosshd.desktop.lifecycle;

import android.app.*;
import android.content.*;
import android.os.*;
import com.gosshd.desktop.local.LocalState;
import com.gosshd.desktop.rpc.BridgeServer;
import com.gosshd.desktop.ui.MainActivity;

// Foreground lifecycle is local opt-in. STOP remains effective across restarts.
public final class ControlService extends Service {
  public static final String STOP = "com.gosshd.desktop.STOP";
  private BridgeServer bridge;
  private PowerManager.WakeLock lock;
  private int clients;

  public static void start(Context context) {
    try {
      context.startForegroundService(new Intent(context, ControlService.class));
    } catch (RuntimeException ignored) {
      /* owner can start from the visible activity */
    }
  }

  @Override
  public void onCreate() {
    super.onCreate();
    NotificationManager manager = getSystemService(NotificationManager.class);
    manager.createNotificationChannel(
        new NotificationChannel(
            "desktop-control", "Desktop control", NotificationManager.IMPORTANCE_LOW));
    lock =
        getSystemService(PowerManager.class)
            .newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "gosshd:desktop-session");
    lock.setReferenceCounted(false);
  }

  @Override
  public int onStartCommand(Intent intent, int flags, int startId) {
    LocalState state = new LocalState(this);
    if (intent != null && STOP.equals(intent.getAction())) {
      state.enabled(false);
      stopSelf();
      return START_NOT_STICKY;
    }
    if (!state.enabled()) {
      stopSelf();
      return START_NOT_STICKY;
    }
    startForeground(41, notification());
    if (bridge == null) {
      try {
        bridge =
            new BridgeServer(
                state,
                delta ->
                    new Handler(getMainLooper())
                        .post(
                            () -> {
                              clients = Math.max(0, clients + delta);
                              if (clients > 0) lock.acquire(10 * 60 * 1000L);
                              else if (lock.isHeld()) lock.release();
                              getSystemService(NotificationManager.class)
                                  .notify(41, notification());
                            }));
      } catch (Exception e) {
        stopSelf();
        return START_NOT_STICKY;
      }
    }
    return START_STICKY;
  }

  private Notification notification() {
    PendingIntent open =
        PendingIntent.getActivity(
            this,
            0,
            new Intent(this, MainActivity.class),
            PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);
    PendingIntent stop =
        PendingIntent.getService(
            this,
            1,
            new Intent(this, ControlService.class).setAction(STOP),
            PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);
    return new Notification.Builder(this, "desktop-control")
        .setSmallIcon(android.R.drawable.ic_menu_view)
        .setContentTitle("GOSSHD Desktop control enabled")
        .setContentText(clients + " paired local session(s); stop anytime")
        .setContentIntent(open)
        .setOngoing(true)
        .addAction(new Notification.Action.Builder(null, "Stop control", stop).build())
        .build();
  }

  @Override
  public void onDestroy() {
    if (bridge != null) bridge.close();
    if (lock != null && lock.isHeld()) lock.release();
    super.onDestroy();
  }

  @Override
  public IBinder onBind(Intent intent) {
    return null;
  }
}
