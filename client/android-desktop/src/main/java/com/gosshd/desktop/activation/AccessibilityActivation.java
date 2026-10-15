package com.gosshd.desktop.activation;

import android.content.ComponentName;
import android.content.Context;
import android.provider.Settings;
import com.gosshd.desktop.access.DesktopAccessibilityService;
import java.util.LinkedHashSet;

// Activation is invoked only by a local app button after a grant. Never RPC.
public final class AccessibilityActivation {
  public static String component(Context context) {
    return new ComponentName(context, DesktopAccessibilityService.class).flattenToString();
  }

  public static String merge(String current, String own) {
    LinkedHashSet<String> items = new LinkedHashSet<>();
    if (current != null && !current.equals("null")) {
      for (String v : current.split(":")) {
        if (!v.trim().isEmpty()) items.add(v);
      }
    }
    items.add(own);
    return String.join(":", items);
  }

  public static void activate(Context context) {
    String current =
        Settings.Secure.getString(
            context.getContentResolver(), Settings.Secure.ENABLED_ACCESSIBILITY_SERVICES);
    if (!Settings.Secure.putString(
        context.getContentResolver(),
        Settings.Secure.ENABLED_ACCESSIBILITY_SERVICES,
        merge(current, component(context))))
      throw new IllegalStateException("Cannot update enabled services");
    if (!Settings.Secure.putInt(
        context.getContentResolver(), Settings.Secure.ACCESSIBILITY_ENABLED, 1))
      throw new IllegalStateException("Cannot enable accessibility");
  }
}
