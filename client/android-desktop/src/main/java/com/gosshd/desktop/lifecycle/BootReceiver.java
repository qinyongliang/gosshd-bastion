package com.gosshd.desktop.lifecycle;

import android.content.*;
import com.gosshd.desktop.local.LocalState;

public final class BootReceiver extends BroadcastReceiver {
  public void onReceive(Context context, Intent intent) {
    LocalState state = new LocalState(context);
    if (Intent.ACTION_BOOT_COMPLETED.equals(intent.getAction()) && state.boot() && state.enabled())
      ControlService.start(context);
  }
}
