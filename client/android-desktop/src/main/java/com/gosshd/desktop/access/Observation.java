package com.gosshd.desktop.access;

import android.graphics.Rect;
import android.view.accessibility.AccessibilityNodeInfo;
import java.util.ArrayList;

// Private node references live only in one authenticated session/observation.
public final class Observation {
  public String id, app;
  public int window;
  public Rect bounds, display;
  public boolean buttonIsLeft = true;
  public long at;
  public final ArrayList<AccessibilityNodeInfo> nodes = new ArrayList<>();

  public void clear() {
    for (AccessibilityNodeInfo node : nodes) node.recycle();
    nodes.clear();
    id = null;
  }
}
