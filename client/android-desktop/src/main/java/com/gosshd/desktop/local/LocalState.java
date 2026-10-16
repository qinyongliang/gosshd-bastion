package com.gosshd.desktop.local;

import android.content.Context;
import android.content.SharedPreferences;
import android.util.Base64;
import java.security.SecureRandom;

public final class LocalState {
  private final SharedPreferences prefs;

  public LocalState(Context context) {
    prefs = context.getSharedPreferences("desktop", Context.MODE_PRIVATE);
  }

  public boolean enabled() {
    return prefs.getBoolean("enabled", false);
  }

  public void enabled(boolean value) {
    prefs.edit().putBoolean("enabled", value).apply();
  }

  public boolean agentEnabled() { return prefs.getBoolean("agent-enabled", false); }

  public void agentEnabled(boolean value) { prefs.edit().putBoolean("agent-enabled", value).apply(); }

  public boolean shouldRun() { return enabled() || agentEnabled(); }

  public void stopAll() { prefs.edit().putBoolean("enabled", false).putBoolean("agent-enabled", false).commit(); }

  public boolean boot() {
    return prefs.getBoolean("boot", false);
  }

  public void boot(boolean value) {
    prefs.edit().putBoolean("boot", value).apply();
  }

  public String token() {
    String value = prefs.getString("token", null);
    if (value == null) {
      value = rotate();
    }
    return value;
  }

  public String rotate() {
    byte[] bytes = new byte[32];
    new SecureRandom().nextBytes(bytes);
    String value =
        Base64.encodeToString(bytes, Base64.URL_SAFE | Base64.NO_PADDING | Base64.NO_WRAP);
    prefs.edit().putString("token", value).commit();
    return value;
  }
}
