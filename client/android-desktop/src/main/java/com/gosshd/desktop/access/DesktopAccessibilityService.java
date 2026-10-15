package com.gosshd.desktop.access;

import android.accessibilityservice.AccessibilityService;
import android.view.accessibility.AccessibilityEvent;

public final class DesktopAccessibilityService extends AccessibilityService {
  private static volatile DesktopAccessibilityService connected;

  public static DesktopAccessibilityService connected() {
    return connected;
  }

  @Override
  protected void onServiceConnected() {
    connected = this;
  }

  @Override
  public void onAccessibilityEvent(AccessibilityEvent event) {}

  @Override
  public void onInterrupt() {}

  @Override
  public void onDestroy() {
    connected = null;
    super.onDestroy();
  }
}
