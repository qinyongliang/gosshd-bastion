package com.gosshd.desktop.lifecycle;

import android.content.*;
import com.gosshd.desktop.local.LocalState;

public final class BootReceiver extends BroadcastReceiver {
  public void onReceive(Context context, Intent intent) {
    LocalState state = new LocalState(context);
    boolean boot = Intent.ACTION_BOOT_COMPLETED.equals(intent.getAction()) && state.boot();
    boolean update = Intent.ACTION_MY_PACKAGE_REPLACED.equals(intent.getAction());
    if ((boot || update) && state.shouldRun()) ControlService.start(context);
  }
}
